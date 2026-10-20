package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	account "kero/internal/usage"
)

func accountUsage(args []string) error {
	f, err := parse(args, nil, map[string]string{"json": "json"})
	if err != nil {
		return err
	}
	if len(f.pos) > 0 {
		return usageErr("kero usage [--json]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reports := account.Refresh(ctx, nil)
	if f.set["json"] {
		return printJSON(reports)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tUSAGE\tRESET / STATUS")
	for _, r := range reports {
		if r.Error != "" {
			fmt.Fprintf(w, "%s\t—\t%s\n", r.Name, r.Error)
			continue
		}
		if r.Balance != "" {
			fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Balance, r.Plan)
			continue
		}
		for _, window := range r.Windows {
			reset := "Unknown"
			if !window.Resets.IsZero() {
				reset = window.Resets.Local().Format(time.RFC822)
			}
			fmt.Fprintf(w, "%s · %s\t%.0f%% used\t%s\n", r.Name, strings.ToLower(window.Label), window.Used, reset)
		}
	}
	return w.Flush()
}
