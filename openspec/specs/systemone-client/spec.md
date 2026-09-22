# systemone-client Specification

## Purpose

Send typed System One questions to one selected Jev provider and return only judgments the provider actually produced.

## Requirements

### Requirement: Provider selection
The client SHALL evaluate System One requests through exactly one selected provider per call: `typesafe` or `opencode-go`. `typesafe` MUST send `POST https://api.typesafe.ai/v1/systemone` with `Authorization: Bearer` taken from `TYPESAFE_API_KEY`. `opencode-go` MUST send `POST https://opencode.ai/zen/v1/systemone` with `Authorization: Bearer` taken from `OPENCODE_GO_API_KEY` when that variable is set, and from `OPENCODE_API_KEY` otherwise. The provider id `opencode-zen` MUST be accepted as an alias of `opencode-go`. The client MUST NOT send a System One request to an OpenCode Go chat-completions URL.

#### Scenario: Official provider
- **WHEN** the provider is `typesafe` and `TYPESAFE_API_KEY` is set
- **THEN** the client posts the request to `https://api.typesafe.ai/v1/systemone` and sets the bearer token from that variable

#### Scenario: OpenCode Go key
- **WHEN** the provider is `opencode-go` and `OPENCODE_GO_API_KEY` is set
- **THEN** the client posts the request to `https://opencode.ai/zen/v1/systemone` and sets the bearer token from that variable

#### Scenario: Console key fallback
- **WHEN** the provider is `opencode-go`, `OPENCODE_GO_API_KEY` is unset, and `OPENCODE_API_KEY` is set
- **THEN** the client posts the request to `https://opencode.ai/zen/v1/systemone` and sets the bearer token from `OPENCODE_API_KEY`

#### Scenario: Go endpoint refused
- **WHEN** configuration sets the System One base URL to an OpenCode Go chat-completions endpoint
- **THEN** the client rejects the configuration before sending and returns a provider error

### Requirement: Shared state and typed questions
The client SHALL send `model`, `state`, and `questions` in one JSON body. `state` MUST be a string, a JSON object, or an array of strings. Each question MUST be a `noul`, `choice`, or `score` and MUST carry its own `instructions`. Question map keys MUST be returned with the matching answers and MUST NOT be treated as the text of the question. One request MUST submit every question that fits in the same state together.

#### Scenario: Parallel questions
- **WHEN** the caller submits one state with a Noul, a Choice, and a Score
- **THEN** the client sends one request and returns each answer under the caller's question key

#### Scenario: Missing instructions
- **WHEN** a question omits `instructions`
- **THEN** the client rejects that question locally and does not send the request

### Requirement: Model identity
For `typesafe`, the client SHALL send `jev-1.13.0` unless the caller sets `jev-latest`. For `opencode-go`, the client SHALL send `jev-1.13` unless the caller sets `jev-1.13-free`. The response MUST include the provider name, the requested model id, and the model version reported by the provider when the provider returns one.

#### Scenario: Default OpenCode Go model
- **WHEN** the provider is `opencode-go` and the caller does not override the model
- **THEN** the request model field is `jev-1.13`

#### Scenario: Reported version
- **WHEN** the provider response names a concrete model version
- **THEN** the client result includes that version separately from the alias that was requested

### Requirement: Request budget
The client SHALL split questions into multiple requests when the estimated tokens of `state` plus `questions` would exceed 32K. The client MUST NOT split a single question across requests. Each result MUST record the estimator name. A split MUST preserve every question key exactly once across the combined result.

#### Scenario: Oversized batch
- **WHEN** the combined estimate exceeds 32K and the questions can be partitioned under that cap
- **THEN** the client sends more than one request and merges the answers by question key

#### Scenario: Single question over the cap
- **WHEN** one question plus its state estimates above 32K
- **THEN** the client returns a budget error for that question and does not send it

### Requirement: No fabricated judgment
The client MUST NOT invent probabilities, labels, or scores when the provider returns an error, a timeout, or a body that does not match the question type. The failure MUST identify the provider and the question keys that were not answered.

#### Scenario: Malformed body
- **WHEN** the provider returns HTTP 200 with a missing probability for a Noul
- **THEN** the client returns a parse error and no probability for that question

#### Scenario: Missing key
- **WHEN** the selected provider's API key is unset
- **THEN** the client returns an authentication error and performs no network call
