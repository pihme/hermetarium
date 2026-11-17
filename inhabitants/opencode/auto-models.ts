/**
 * opencode-auto-models
 *
 * Auto-discovers and syncs model lists for OpenAI-compatible custom providers
 * at opencode startup. No manual model config needed.
 *
 * Understands two /models response shapes:
 *   1. Rich `capabilities` object (vision, pdf, audioInput, contextWindow, ...)
 *   2. Flat fields (context_window, max_output_tokens, input_modalities, ...)
 *
 * Existing local model entries are preserved (manual metadata wins).
 * New models are auto-added from the remote endpoint.
 *
 * Ported from opencode-auto-models 1.0.0 (MIT) to the OpenCode 2 plugin API.
 * deriveLimits fills a missing output limit. A catalog that sends
 * max_output_tokens: null otherwise makes OpenCode reject the model.
 * This file is loaded from the image. It is not an npm plugin entry.
 */

import { readFileSync } from "node:fs"
import { Model, Plugin, Provider } from "@opencode/plugin"

const DEFAULT_TIMEOUT = 10_000
const STARTUP_TIMEOUT = 15_000

const TEXT = "text"
const AUDIO = "audio"
const IMAGE = "image"
const VIDEO = "video"
const PDF = "pdf"

type RawModel = Record<string, any>

// ── Helpers ──────────────────────────────────────────────────────────────────

function log(...args: unknown[]) {
  console.log("[auto-provider-models]", ...args)
}
function warn(...args: unknown[]) {
  console.warn("[auto-provider-models]", ...args)
}

function asBool(...vals: unknown[]): boolean | undefined {
  for (const v of vals) {
    if (typeof v === "boolean") return v
  }
  return undefined
}

function num(v: unknown): number | undefined {
  if (typeof v === "number" && Number.isFinite(v)) return v
  if (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v))) return Number(v)
  return undefined
}

// ── Modalities ───────────────────────────────────────────────────────────────

function parseModalityList(v: unknown): string[] | undefined {
  if (Array.isArray(v)) {
    const out = v.filter((x): x is string => typeof x === "string" && x.length > 0).map((x) => x.trim())
    return out.length ? out : undefined
  }
  if (typeof v === "string" && v.trim()) {
    return v.split(/[\s,|]+/).map((s) => s.trim()).filter(Boolean) || undefined
  }
  return undefined
}

function sanitizeModalities(list: string[] | undefined): string[] | undefined {
  if (!list?.length) return undefined
  const known = new Set([TEXT, AUDIO, IMAGE, VIDEO, PDF])
  const out = [...new Set(list.filter((x) => known.has(x)))]
  return out.length ? out : undefined
}

function deriveModalities(m: RawModel): { input: string[]; output: string[] } | undefined {
  const caps = m?.capabilities && typeof m.capabilities === "object" ? m.capabilities : {}

  let input = sanitizeModalities(parseModalityList(m.input_modalities ?? m.modalities?.input ?? m.input))
  let output = sanitizeModalities(parseModalityList(m.output_modalities ?? m.modalities?.output ?? m.output))

  if (input && !input.includes(TEXT)) input = [TEXT, ...input]
  if (output && !output.includes(TEXT)) output = [TEXT, ...output]

  const addInput: Record<string, boolean | undefined> = {
    [IMAGE]: asBool(caps.vision, m.vision),
    [PDF]: asBool(caps.pdf, m.pdf),
    [AUDIO]: asBool(caps.audioInput, m.audioInput),
    [VIDEO]: asBool(caps.videoInput, m.videoInput),
  }
  const addOutput: Record<string, boolean | undefined> = {
    [IMAGE]: asBool(caps.imageOutput, m.imageOutput),
    [AUDIO]: asBool(caps.audioOutput, m.audioOutput),
  }

  let hasCaps = false
  for (const [token, flag] of Object.entries(addInput)) {
    if (flag === undefined) continue
    hasCaps = true
    if (flag && !input?.includes(token)) input = [...(input ?? [TEXT]), token]
  }
  for (const [token, flag] of Object.entries(addOutput)) {
    if (flag === undefined) continue
    hasCaps = true
    if (flag && !output?.includes(token)) output = [...(output ?? [TEXT]), token]
  }

  if (!input && !output && !hasCaps) return undefined
  return { input: input ?? [TEXT], output: output ?? [TEXT] }
}

// ── Model capabilities ───────────────────────────────────────────────────────

function deriveLimits(m: RawModel): { context?: number; output?: number } | undefined {
  const caps = m?.capabilities && typeof m.capabilities === "object" ? m.capabilities : {}
  let context =
    num(caps.contextWindow) ?? num(m.context_length) ?? num(m.context_window) ?? num(m.contextWindow) ?? num(m.max_input_tokens)
  let output =
    num(caps.maxOutput) ?? num(m.max_completion_tokens) ?? num(m.max_output_tokens) ?? num(m.maxOutputTokens) ?? num(caps.max_tokens)
  if (context === undefined && output === undefined) return undefined
  if (output === undefined) output = Math.min(context ?? 8192, 8192)
  if (context === undefined) context = Math.max(output, 8192)
  return { context, output }
}

function toolCallEnabled(m: RawModel, caps: Record<string, any>): boolean | undefined {
  return asBool(caps.tools, m.tools, m.tool_call) === false ? false : undefined
}

// ── Fetch & sync ─────────────────────────────────────────────────────────────

function normalizeBaseURL(baseURL: string): string {
  return baseURL.replace(/\/+$/, "")
}

async function fetchModels(baseURL: string, apiKey: string | undefined, timeoutMs: number): Promise<RawModel[]> {
  const url = `${normalizeBaseURL(baseURL)}/models`
  const headers: Record<string, string> = { Accept: "application/json" }
  if (apiKey) headers.Authorization = `Bearer ${apiKey}`
  const res = await fetch(url, { headers, signal: AbortSignal.timeout(timeoutMs) })
  if (!res.ok) throw new Error(`GET ${url} -> HTTP ${res.status}`)
  const payload = (await res.json()) as { data?: RawModel[] }
  if (!Array.isArray(payload?.data)) throw new Error(`GET ${url} -> no data[]`)
  return payload.data
}

function resolveApiKey(providerConfig: Record<string, any>, env: Record<string, string | undefined>) {
  const configured = providerConfig?.settings?.apiKey ?? providerConfig?.options?.apiKey
  if (typeof configured === "string" && configured.trim() && !configured.includes("{env:"))
    return configured.trim()
  const envNames: string[] = Array.isArray(providerConfig?.env) ? providerConfig.env : []
  for (const name of envNames) {
    const v = env[name]
    if (typeof v === "string" && v.trim()) return v.trim()
  }
  return undefined
}

// ── Provider auto-detection ──────────────────────────────────────────────────

type FileProvider = {
  package?: string
  npm?: string
  settings?: { baseURL?: string; apiKey?: string }
  options?: { baseURL?: string; apiKey?: string }
  env?: string[]
}

function baseURLOf(p: FileProvider): string {
  const fromSettings = typeof p.settings?.baseURL === "string" ? p.settings.baseURL : ""
  const fromOptions = typeof p.options?.baseURL === "string" ? p.options.baseURL : ""
  return (fromSettings || fromOptions).trim()
}

function compatible(p: FileProvider): boolean {
  const pkg = `${p.package ?? ""} ${p.npm ?? ""}`
  return pkg.includes("openai-compatible")
}

function providersIn(text: string): Array<{ id: string; baseURL: string; apiKey?: string }> {
  let doc: { providers?: Record<string, FileProvider>; provider?: Record<string, FileProvider> }
  try {
    doc = JSON.parse(text)
  } catch (e) {
    warn(`config is not JSON: ${e instanceof Error ? e.message : String(e)}`)
    return []
  }
  const providers = doc.providers ?? doc.provider
  if (!providers || typeof providers !== "object") return []
  const env = process.env as Record<string, string | undefined>
  const out: Array<{ id: string; baseURL: string; apiKey?: string }> = []
  for (const [id, p] of Object.entries(providers)) {
    if (!p || !compatible(p)) continue
    const baseURL = baseURLOf(p)
    if (!baseURL) continue
    out.push({ id, baseURL, apiKey: resolveApiKey(p as Record<string, any>, env) })
  }
  return out
}

function readConfigProviders(): Array<{ id: string; baseURL: string; apiKey?: string }> {
  const home = process.env.HOME || "/root"
  const path = `${home}/.config/opencode/opencode.json`
  try {
    const text = readFileSync(path, "utf8")
    return providersIn(text)
  } catch (e) {
    warn(`no config at ${path}: ${e instanceof Error ? e.message : String(e)}`)
    return []
  }
}

function toInfo(providerID: ReturnType<typeof Provider.ID.make>, remoteId: string, m: RawModel): Model.Info {
  const info = Model.Info.default(providerID, Model.ID.make(remoteId))
  const name = typeof m.name === "string" && m.name.trim() ? m.name.trim() : remoteId
  const limits = deriveLimits(m)
  const mods = deriveModalities(m)
  const caps = m?.capabilities && typeof m.capabilities === "object" ? m.capabilities : {}
  const tools = toolCallEnabled(m, caps)
  return {
    ...info,
    name,
    limit: {
      context: (limits?.context ?? info.limit.context) || 8192,
      output: (limits?.output ?? info.limit.output) || 8192,
    },
    capabilities: {
      ...info.capabilities,
      ...(mods ? { input: mods.input, output: mods.output } : {}),
      ...(tools === false ? { tools: false } : {}),
    },
  }
}

// OpenCode 2 plugin. Reads the image config, fetches {baseURL}/models, and
// adds those models beside the ones already configured. A failed fetch leaves
// the configured models in place.
export default Plugin.define({
  id: "hermetarium.auto-models",
  async setup(ctx) {
    const targets = readConfigProviders()
    if (targets.length === 0) {
      log("no providers to sync")
      return
    }
    log(`auto-detected providers: ${targets.map((p) => p.id).join(", ")}`)
    const synced: Array<{ id: string; models: Model.Info[] }> = []
    const work = targets.map(async (target) => {
      try {
        const remote = await fetchModels(target.baseURL, target.apiKey, DEFAULT_TIMEOUT)
        const providerID = Provider.ID.make(target.id)
        synced.push({
          id: target.id,
          models: remote.flatMap((m) => {
            const remoteId = typeof m?.id === "string" ? m.id.trim() : ""
            return remoteId ? [toInfo(providerID, remoteId, m)] : []
          }),
        })
        log(`provider ${target.id}: fetched ${remote.length} remote`)
      } catch (e) {
        warn(`failed to fetch models for ${target.id}: ${e instanceof Error ? e.message : String(e)}`)
      }
    })
    await Promise.race([Promise.all(work), new Promise((r) => setTimeout(r, STARTUP_TIMEOUT))])
    if (synced.length === 0) return
    // provider.transform lists the public catalog. A custom provider such as
    // hermetarium is not in that list, so models.set never lands. model.transform
    // can add a model under a provider that is already configured.
    await ctx.model.transform((editor) => {
      for (const s of synced) {
        let added = 0
        for (const model of s.models) {
          if (editor.get(s.id, model.id)) continue
          editor.update(s.id, model.id, (draft) => {
            draft.name = model.name
            draft.limit = { context: model.limit.context, output: model.limit.output }
            draft.capabilities = {
              ...draft.capabilities,
              input: model.capabilities.input,
              output: model.capabilities.output,
              ...(model.capabilities.tools === false ? { tools: false } : {}),
            }
            draft.enabled = true
          })
          added++
        }
        log(`provider ${s.id}: added ${added}`)
      }
    })
  },
})
