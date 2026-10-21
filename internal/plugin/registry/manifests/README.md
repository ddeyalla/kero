# Built-in plugin manifests

Each built-in plugin puts its `kero-plugin.json` here. The file is embedded
in the app, and `kero plugin-run <id>` starts it. The plugin's Go package
registers the code that serves it with `registry.RegisterRunner` in its
`init`; the app imports that package for its side effect.

The Spotify plugin's manifest is here. Usage arrives with its own ticket.
