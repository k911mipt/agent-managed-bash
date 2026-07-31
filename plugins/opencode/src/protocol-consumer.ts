import type {
  ExitClassContract,
  PersistedJobState,
  Request,
  Response,
} from "./generated/protocol.gen"

type Assert<Condition extends true> = Condition
type IsAny<Value> = 0 extends 1 & Value ? true : false
type IsExact<Left, Right> =
  (<Value>() => Value extends Left ? 1 : 2) extends <Value>() => Value extends Right ? 1 : 2
    ? true
    : false
type RequiredKeys<Value> = {
  [Key in keyof Value]-?: Record<never, never> extends Pick<Value, Key> ? never : Key
}[keyof Value]
type StartRequest = Extract<Request, { action: "start" }>
type RunRequest = Extract<Request, { action: "run" }>
type WaitRequest = Extract<Request, { action: "wait" }>
type StatusRequest = Extract<Request, { action: "status" }>
type OutputRequest = Extract<Request, { action: "output" }>
type CancelRequest = Extract<Request, { action: "cancel" }>
type RemoveRequest = Extract<Request, { action: "remove" }>
type ListRequest = Extract<Request, { action: "list" }>
type VersionRequest = Extract<Request, { action: "version" }>
type ErrorResponse = Extract<Response, { ok: false }>
type StartResponse = Extract<Response, { action: "start" }>
type RunResponse = Extract<Response, { action: "run" }>
type StatusResponse = Extract<Response, { action: "status" }>
type WaitResponse = Extract<Response, { action: "wait" }>
type OutputResponse = Extract<Response, { action: "output" }>
type CancelResponse = Extract<Response, { action: "cancel" }>
type RemoveResponse = Extract<Response, { action: "remove" }>
type ListResponse = Extract<Response, { action: "list" }>
type VersionResponse = Extract<Response, { action: "version" }>

export type GeneratedRunCommandIsTyped = Assert<
  IsExact<RunRequest["payload"]["command"], string>
>
export type GeneratedStartFieldsAreRequired = Assert<
  IsExact<RequiredKeys<StartRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedRunCommandIsNotAny = Assert<
  IsAny<RunRequest["payload"]["command"]> extends false ? true : false
>
export type GeneratedRunFieldsAreRequired = Assert<
  IsExact<RequiredKeys<RunRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedWaitFieldsAreRequired = Assert<
  IsExact<RequiredKeys<WaitRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedStatusFieldsAreRequired = Assert<
  IsExact<RequiredKeys<StatusRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedOutputFieldsAreRequired = Assert<
  IsExact<RequiredKeys<OutputRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedCancelFieldsAreRequired = Assert<
  IsExact<RequiredKeys<CancelRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedRemoveFieldsAreRequired = Assert<
  IsExact<RequiredKeys<RemoveRequest>, "schema_version" | "action" | "context" | "payload">
>
export type GeneratedListFieldsAreRequired = Assert<
  IsExact<RequiredKeys<ListRequest>, "schema_version" | "action" | "context">
>
export type GeneratedVersionFieldsAreRequired = Assert<
  IsExact<RequiredKeys<VersionRequest>, "schema_version" | "action">
>
export type GeneratedRunResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<RunResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedStartResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<StartResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedWaitResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<WaitResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedStatusResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<StatusResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedOutputResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<OutputResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedCancelResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<CancelResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedRemoveResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<RemoveResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedListResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<ListResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedVersionResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<VersionResponse>, "schema_version" | "ok" | "action" | "result">
>
export type GeneratedErrorResponseFieldsAreRequired = Assert<
  IsExact<RequiredKeys<ErrorResponse>, "schema_version" | "ok" | "error">
>
export type GeneratedErrorCodeIsNotAny = Assert<
  IsAny<ErrorResponse["error"]["code"]> extends false ? true : false
>
export type GeneratedExitClassIsExact = Assert<
  IsExact<ExitClassContract["exit_class"], 0 | 2 | 3 | 4 | 5>
>
export type GeneratedRequestActionsAreComplete = Assert<
  IsExact<Request["action"], "start" | "run" | "wait" | "status" | "output" | "cancel" | "remove" | "list" | "version">
>
export type GeneratedResponseActionsAreComplete = Assert<
  IsExact<Exclude<Response["action"], undefined>, "start" | "run" | "wait" | "status" | "output" | "cancel" | "remove" | "list" | "version">
>
export type GeneratedStateFieldsAreRequired = Assert<
  PersistedJobState extends {
    schema_version: 1
    session: { session_id: string }
    job: { job_id: string; status: string }
    observers: readonly { session_id: string; cursor_bytes: number }[]
  }
    ? true
    : false
>
export type GeneratedStatusObservationIsTyped = Assert<
  IsExact<NonNullable<StatusResponse["result"]["process_result"]>["status"],
    | "succeeded"
    | "nonzero_exit"
    | "signal_exit"
    | "cancelled"
    | "hard_timeout"
    | "output_limit"
    | "runner_lost">
>
export type GeneratedWaitOutputIsTyped = Assert<
  IsExact<WaitResponse["result"]["output"]["next_cursor_bytes"], number>
>
export type GeneratedOutputObservationIsTyped = Assert<
  IsExact<OutputResponse["result"]["observation"]["job"]["job_id"], string>
>

export function requestSubject(request: Request): string {
  switch (request.action) {
    case "start":
      return request.payload.command
    case "run":
      return request.payload.command
    case "wait":
      return request.payload.job_id
    case "status":
      return request.payload.job_id
    case "output":
      return request.payload.job_id
    case "cancel":
      return request.payload.job_id
    case "remove":
      return request.payload.job_id
    case "list":
      return request.context.workspace_path
    case "version":
      return "managed-bash"
    default:
      return assertNever(request)
  }
}

function assertNever(value: never): never {
  throw new TypeError(`unreachable request: ${String(value)}`)
}
