/**
 * File Trigger extension — the external wake for the Pi coordinator.
 *
 * Watches /tmp/agent-trigger.txt and injects its contents into the running
 * session as an external event. The write can come from any separate process
 * (a PR-label watcher, a CI check, a cron/tini loop) and needs no human input.
 *
 * This is the mechanism that proves the "notify the coordinator main session"
 * path: pi.sendMessage({triggerTurn:true}) renders the message in the live TUI
 * and starts a model turn (follow-up work). After reading, the file is cleared
 * so the same event does not re-fire.
 *
 * Robustness (review follow-ups):
 * - fs.watch can fire twice for one write; a debounce coalesces the events so
 *   one write triggers one turn, and the last-triggered content is remembered
 *   so a re-delivered event with unchanged content does not re-fire.
 * - fs.watch can stop firing after the watched file is replaced or renamed; a
 *   polling fallback re-reads the file on an interval and re-arms the watcher.
 *
 * Usage from a watcher process:
 *   echo "PR#74 was labeled ready-for-qa - investigate" > /tmp/agent-trigger.txt
 */
import * as fs from "node:fs";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const TRIGGER_FILE = process.env.PI_TRIGGER_FILE || "/tmp/agent-trigger.txt";
// Debounce window for one write (ms). fs.watch may emit several events for a
// single write; events inside this window coalesce into one trigger.
const DEBOUNCE_MS = 250;
// Polling fallback interval (ms): re-reads the file even when fs.watch has
// stopped (e.g. after the file was replaced or renamed).
const POLL_MS = 2000;

export default function (pi: ExtensionAPI) {
	pi.on("session_start", async (_event, ctx) => {
		// Make sure the file exists so fs.watch has something to attach to.
		if (!fs.existsSync(TRIGGER_FILE)) fs.writeFileSync(TRIGGER_FILE, "");

		let lastTriggered = "";
		let timer: ReturnType<typeof setTimeout> | null = null;

		const fire = () => {
			timer = null;
			try {
				const content = fs.readFileSync(TRIGGER_FILE, "utf-8").trim();
				if (!content || content === lastTriggered) return;
				lastTriggered = content;
				pi.sendMessage(
					{
						customType: "file-trigger",
						content: `External trigger: ${content}`,
						display: true,
					},
					{ triggerTurn: true },
				);
				// Clear the file so the same event does not re-fire; the write
				// of the empty file itself is debounced and ignored.
				fs.writeFileSync(TRIGGER_FILE, "");
			} catch {
				// File might not exist yet.
			}
		};

		// Debounce: coalesce the burst of fs.watch events for one write into a
		// single fire. Re-arming is implicit: a later event schedules again.
		const schedule = () => {
			if (timer) clearTimeout(timer);
			timer = setTimeout(fire, DEBOUNCE_MS);
		};

		// fs.watch on a file can stop after a replace/rename; re-arm it whenever
		// an event arrives, and fall back to polling to catch silent changes.
		let watcher: fs.FSWatcher | null = null;
		const arm = () => {
			watcher?.close();
			try {
				watcher = fs.watch(TRIGGER_FILE, schedule);
			} catch {
				watcher = null;
			}
		};
		arm();

		// Polling fallback: if fs.watch stopped (file replaced or renamed), the
		// interval still reads the file. It only fires on a content change.
		const poller = setInterval(() => {
			arm(); // re-arm in case the watcher died with a replaced file
			schedule();
		}, POLL_MS);

		if (ctx.hasUI) {
			ctx.ui.notify(`Watching ${TRIGGER_FILE}`, "info");
		}

		// session_shutdown: stop the timers and watcher when the session ends.
		pi.on("session_shutdown", () => {
			if (timer) clearTimeout(timer);
			clearInterval(poller);
			watcher?.close();
		});
	});
}
