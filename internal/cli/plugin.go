package cli

import (
	"errors"
	"fmt"

	"kero/internal/client"
	"kero/internal/plugin/registry"
	"kero/internal/proto"
)

// pluginCmd pins widgets of plugins to the app's sidebar, or takes them
// off it. The widget is the plugin's first one unless it is named.
//
//	kero plugin pin <plugin> [widget]
//	kero plugin unpin <plugin> [widget]
func pluginCmd(args []string) error {
	if len(args) < 1 || (args[0] != "pin" && args[0] != "unpin") || len(args) < 2 || len(args) > 3 {
		return usageErr("kero plugin pin|unpin <plugin> [widget]")
	}
	action, kv, err := pinAction(args[0], args[1:], knownPlugins())
	if err != nil {
		return err
	}
	c, err := client.Connect(true)
	if err != nil {
		return errUnreachable
	}
	defer c.Close()
	var delivered bool
	if err := c.Call("do", proto.DoParams{Action: action, Args: kv}, &delivered); err != nil {
		return err
	}
	if !delivered {
		return errors.New("the app is not open")
	}
	return nil
}

// pinAction is the action that pins or unpins a widget, with its arguments,
// after checking that the plugin is one the app knows and has the widget.
// An unpin takes a pin of a plugin that is gone off as well, so it checks
// nothing.
func pinAction(sub string, args []string, plugins []registry.Plugin) (string, map[string]string, error) {
	kv := map[string]string{"plugin": args[0]}
	if len(args) > 1 {
		kv["widget"] = args[1]
	}
	if sub == "unpin" {
		return "plugin.unpin", kv, nil
	}
	p, ok := registry.Find(plugins, args[0])
	if !ok {
		return "", nil, fmt.Errorf("%w: no plugin %q (see kero actions)", errNotFound, args[0])
	}
	if w := kv["widget"]; w != "" && p.Manifest.WidgetByName(w) == nil {
		return "", nil, fmt.Errorf("%w: plugin %q has no widget %q", errNotFound, args[0], w)
	}
	if len(p.Manifest.Widgets) == 0 {
		return "", nil, fmt.Errorf("%w: plugin %q has no widgets", errNotFound, args[0])
	}
	return "plugin.pin", kv, nil
}
