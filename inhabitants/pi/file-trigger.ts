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
 * Usage from a watcher process:
 *   echo "PR#74 was labeled ready-for-qa - investigate" > /tmp/agent-trigger.txt
 */
import * as fs from "node:fs";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	pi.on("session_start", async (_event, ctx) => {
		const triggerFile = "/tmp/agent-trigger.txt";
		// Make sure the file exists so fs.watch has something to attach to.
		if (!fs.existsSync(triggerFile)) fs.writeFileSync(triggerFile, "");

		fs.watch(triggerFile, () => {
			try {
				const content = fs.readFileSync(triggerFile, "utf-8").trim();
				if (content) {
					pi.sendMessage(
						{
							customType: "file-trigger",
							content: `External trigger: ${content}`,
							display: true,
						},
						{ triggerTurn: true },
					);
					fs.writeFileSync(triggerFile, "");
				}
			} catch {
				// File might not exist yet.
			}
		});

		if (ctx.hasUI) {
			ctx.ui.notify(`Watching ${triggerFile}`, "info");
		}
	});
}
