# Supervisor service packaging

`multica-hermes-gateway supervisor` is a foreground process. The ACP Adapter
must not be its parent; production deployments should let the host service
manager own this process.

## macOS launchd

1. Copy `launchd/com.linx.multica-hermes-supervisor.plist.template` to the
   user LaunchAgents directory.
2. Replace the three `__...__` placeholders with the local executable,
   config, and log paths. Set `supervisor.hermes_executable` in the config to
   an absolute Hermes executable path when launchd's restricted PATH cannot
   resolve `hermes`. Do not put a Gateway token in the plist.
3. Load it with `launchctl bootstrap gui/$(id -u) <plist>` and inspect it with
   `launchctl print gui/$(id -u)/com.linx.multica-hermes-supervisor`.

The template uses `KeepAlive` and therefore restarts the Supervisor after a
crash. Runtime recovery is fenced by the SQLite registry and process identity
checks; an unproven PID is never killed.

## Windows Service

Register the same foreground command with the organization's service manager
or a Windows service wrapper:

```text
multica-hermes-gateway.exe supervisor --config <config.yaml>
```

The Windows IPC transport is a user-scoped named pipe. The service and ACP
Adapter must run under the same Windows user when using the default pipe ACL.
