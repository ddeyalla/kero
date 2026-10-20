package resources

import (
	"errors"
	"os"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

var mach struct {
	once         sync.Once
	err          error
	host         uint32
	statistics   func(uint32, int32, unsafe.Pointer, *uint32) int32
	statistics64 func(uint32, int32, unsafe.Pointer, *uint32) int32
}

// The rev1 prefix of Apple's vm_statistics64_data_t, from mach/vm_statistics.h.
// Requesting this prefix works on older macOS versions as well as current ones.
type vmStatistics struct {
	Free, Active, Inactive, Wired             uint32
	Counters                                  [9]uint64
	Purgeable, Speculative                    uint32
	Compressions                              [4]uint64
	Compressor, Throttled, External, Internal uint32
	Uncompressed                              uint64
}

func nativeReader() (func() (reading, error), error) {
	mach.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			mach.err = errors.New("System statistics unavailable")
			return
		}
		var hostSelf func() uint32
		purego.RegisterLibFunc(&hostSelf, lib, "mach_host_self")
		purego.RegisterLibFunc(&mach.statistics, lib, "host_statistics")
		purego.RegisterLibFunc(&mach.statistics64, lib, "host_statistics64")
		mach.host = hostSelf()
	})
	if mach.err != nil {
		return nil, mach.err
	}
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return nil, errors.New("Physical memory size unavailable")
	}
	pageSize := uint64(os.Getpagesize())
	return func() (reading, error) {
		var ticks [4]uint32
		count := uint32(len(ticks))
		if mach.statistics(mach.host, 3, unsafe.Pointer(&ticks[0]), &count) != 0 {
			return reading{}, errors.New("CPU statistics unavailable")
		}
		var vm vmStatistics
		count = uint32(unsafe.Sizeof(vm) / 4)
		if mach.statistics64(mach.host, 4, unsafe.Pointer(&vm), &count) != 0 {
			return reading{}, errors.New("Memory statistics unavailable")
		}
		// Active + wired + compressed physical pages; reclaimable cache is excluded.
		used := (uint64(vm.Active) + uint64(vm.Wired) + uint64(vm.Compressor)) * pageSize
		return reading{ticks: ticks, used: used, total: total}, nil
	}, nil
}
