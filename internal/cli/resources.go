package cli

import (
	"fmt"
	"time"

	"kero/internal/resources"
)

func machineResources(args []string) error {
	f, err := parse(args, nil, map[string]string{"json": "json"})
	if err != nil {
		return err
	}
	if len(f.pos) > 0 {
		return usageErr("kero resources [--json]")
	}
	sampler, err := resources.NewSampler()
	if err != nil {
		return err
	}
	if _, err := sampler.Read(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	s, err := sampler.Read()
	if err != nil {
		return err
	}
	if f.set["json"] {
		return printJSON(s)
	}
	if s.CPU != nil {
		fmt.Printf("CPU %.0f%%\n", *s.CPU)
	} else {
		fmt.Println("CPU unavailable")
	}
	fmt.Printf("RAM %.1f / %.1f GiB\n", float64(s.MemoryUsed)/(1<<30), float64(s.MemoryTotal)/(1<<30))
	return nil
}
