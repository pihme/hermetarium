// Test harness for the file-trigger wake extension. Run under Node with
// --experimental-strip-types. It drives the extension with a fake Pi API and
// asserts that writing the trigger file wakes exactly once per distinct event.
//
// Usage:
//   node --experimental-strip-types wake-test.mjs <path-to-file-trigger.ts> <trigger-file>
import fs from "node:fs";
import { pathToFileURL } from "node:url";

const [, , extPath, triggerFile] = process.argv;

// The extension reads PI_TRIGGER_FILE at module load; set it before the
// dynamic import below so the test drives a private trigger file.
process.env.PI_TRIGGER_FILE = triggerFile;

// ---- Fake Pi API -------------------------------------------------------------
function makeFakePi() {
	const handlers = {};
	const calls = { sendMessage: [], notify: [] };
	const pi = {
		on(name, fn) {
			handlers[name] = fn;
		},
		sendMessage(msg, opts) {
			calls.sendMessage.push({ msg, opts });
		},
		ui: {
			notify(text, level) {
				calls.notify.push({ text, level });
			},
		},
	};
	return { pi, handlers, calls };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Give the poll fallback (2000ms) time to notice a replaced file.
const POLL_SLACK = 2300;

async function main() {
	const mod = await import(pathToFileURL(extPath).href);
	const { pi, handlers, calls } = makeFakePi();

	// session_start must have been registered by the module factory.
	mod.default(pi);
	if (typeof handlers.session_start !== "function") {
		throw new Error("file-trigger did not register a session_start handler");
	}

	await handlers.session_start({}, { hasUI: true, ui: pi.ui });

	// session_shutdown is registered by session_start.
	if (typeof handlers.session_shutdown !== "function") {
		throw new Error("file-trigger did not register a session_shutdown handler");
	}

	// The trigger file must exist (the extension creates it).
	await sleep(100);

	// 1) One write -> exactly one wake.
	fs.writeFileSync(triggerFile, "PR#74 was labeled ready-for-qa");
	await sleep(400); // longer than the debounce (250ms)
	if (calls.sendMessage.length !== 1) {
		throw new Error(`expected 1 sendMessage after one write, got ${calls.sendMessage.length}`);
	}
	if (!calls.sendMessage[0].msg.content.includes("PR#74 was labeled ready-for-qa")) {
		throw new Error(`sendMessage did not carry the trigger content: ${calls.sendMessage[0].msg.content}`);
	}
	if (calls.sendMessage[0].opts.triggerTurn !== true) {
		throw new Error("sendMessage did not request a turn");
	}

	// The file is cleared after reading (so the event does not re-fire).
	await sleep(100);
	if (fs.readFileSync(triggerFile, "utf-8") !== "") {
		throw new Error("trigger file was not cleared after the wake");
	}

	// 2) A burst of events for the same write (fs.watch can fire twice) still
	// wakes once. Simulate by writing and then touching again quickly.
	fs.writeFileSync(triggerFile, "second event");
	await sleep(50);
	fs.writeFileSync(triggerFile, "second event"); // duplicate write, same content
	await sleep(400);
	if (calls.sendMessage.length !== 2) {
		throw new Error(`expected 2 sendMessage (dedupe a duplicate write), got ${calls.sendMessage.length}`);
	}

	// 3) A watcher that stops firing (file replaced) still wakes via the poll
	// fallback: remove the file (killing the watcher), then write, and wait
	// past one poll interval for the fallback to re-read the file.
	fs.rmSync(triggerFile);
	await sleep(POLL_SLACK);
	fs.writeFileSync(triggerFile, "third event after replace");
	await sleep(POLL_SLACK + 400); // one full poll cycle after the write
	if (calls.sendMessage.length !== 3) {
		throw new Error(`expected 3 sendMessage (poll fallback after replace), got ${calls.sendMessage.length}`);
	}

	// 4) session_shutdown stops further wakes.
	await handlers.session_shutdown({}, {});
	const before = calls.sendMessage.length;
	fs.writeFileSync(triggerFile, "after shutdown");
	await sleep(300);
	if (calls.sendMessage.length !== before) {
		throw new Error("extension kept waking after session_shutdown");
	}

	console.log("WAKE_ONCE_OK", JSON.stringify({ wakes: before }));
	process.exit(0);
}

main().catch((err) => {
	console.error("WAKE_TEST_FAIL", err && err.message ? err.message : String(err));
	process.exit(1);
});
