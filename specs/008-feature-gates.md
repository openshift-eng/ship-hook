# Feature Gates

## Status
Draft

## Motivation

New non-trivial functionality needs a way to ship safely: initially off by default so it can be tested in staging, then on by default once confidence is established, and finally removed once the feature is stable. Environment variables are a natural fit for gating in Kubernetes — changing an envvar in a Deployment spec triggers a re-deploy, giving operators a simple toggle without config file changes.

This is distinct from the plugin enable/disable mechanism in `pkg/config/`, which controls whether a sub-plugin receives events at all. Feature gates control behavior _within_ a sub-plugin (or any other code path) that is already enabled.

## Design

### Lifecycle

A feature gate progresses through three phases:

1. **Opt-in**: gate defaults OFF. Operators set the envvar to `true` to activate.
2. **Default on**: gate defaults ON. Operators can set the envvar to `false` to deactivate.
3. **Removed**: gate definition and branching code are deleted. The envvar becomes inert.

### Package: `pkg/featuregate`

A `Gates` struct holds all gate definitions and provides an `Enabled(name)` method. It is created once at startup and injected into consumers via constructors, following the project's dependency injection pattern.

```go
type DefaultState bool

const (
    DefaultOff DefaultState = false
    DefaultOn  DefaultState = true
)

type Spec struct {
    Name    string
    Default DefaultState
}
```

Constructor takes an environment lookup function and gate specs:

```go
func New(envLookup func(string) (string, bool), specs ...Spec) *Gates
```

The `envLookup` parameter replaces direct `os.LookupEnv` calls, making tests trivial — pass a fake function that returns whatever the test needs. No `t.Setenv`, no global state, no cleanup.

### Environment Variable Convention

Gate names are kebab-case (e.g., `auto-assign-reviewers`). The corresponding envvar is derived automatically:

```
auto-assign-reviewers  →  SHIP_HOOK_FEATURE_AUTO_ASSIGN_REVIEWERS
```

Rule: uppercase, replace `-` with `_`, prepend `SHIP_HOOK_FEATURE_`.

### Envvar Interpretation

- `true` or `1`: gate is ON regardless of default
- `false` or `0`: gate is OFF regardless of default
- Unset or any other value: default applies

### Gate Registry

All gates are defined in `pkg/featuregate/gates.go` — a single file that is the only place touched when adding, graduating, or removing a gate. Starts empty:

```go
var AllGates = []Spec{}
```

### Startup Logging

`Gates.LogStates(logger)` is called once at startup, logging each gate's name, default, envvar name, and effective state. This gives operators a clear picture in pod logs.

### Concurrency

The `Gates` struct is immutable after construction. `Enabled()` reads immutable specs and calls `envLookup` (which for `os.LookupEnv` is safe for concurrent reads). No mutex needed.

### Interaction with Config

A gated feature may introduce new config sections in a sub-plugin's `PluginConfig`. No special config-gate coupling is needed:

- Config structs include gated fields unconditionally. Go zero-values make them safe when unused.
- Code that reads gated config lives inside the `gates.Enabled(...)` branch.
- Operators can pre-configure gated features in YAML before flipping the gate envvar.
- `ResolvePluginConfig` (using `json.Unmarshal`) does not reject unused fields, so gated config sections coexist harmlessly.

Gate graduation only requires changes to `gates.go` and the branching code — not config structs or YAML files.

### Integration

In `cmd/ship-hook/main.go` and `cmd/devserver/main.go`, after config loading:

```go
gates := featuregate.New(os.LookupEnv, featuregate.AllGates...)
gates.LogStates(logger)
```

Sub-plugins that need gates accept `*featuregate.Gates` in their constructor and call `gates.Enabled("gate-name")` in handlers.

### Testing Sub-Plugins with Gates

Construct a real `*Gates` with a fake env function:

```go
gates := featuregate.New(
    func(key string) (string, bool) { return "true", true },
    featuregate.Spec{Name: "my-gate", Default: featuregate.DefaultOff},
)
plugin := myplugin.New(ghc, resolver, gates)
```

No interface needed — the concrete struct with injected `envLookup` is directly testable.

## Verification

1. `make test` — featuregate unit tests pass (default on/off, envvar override, unknown gate)
2. `make vet` — no issues
3. `make build` — compiles with gates wired in both main.go and devserver
