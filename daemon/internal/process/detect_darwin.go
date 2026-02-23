// Package process implements macOS-specific foreground process detection
// using sysctl(KERN_PROC) and proc_pidpath().
package process

/*
#include <sys/sysctl.h>
#include <libproc.h>
#include <string.h>
#include <stdlib.h>

// getChildPIDs returns child PIDs for a given parent PID.
// Returns the count of children found.
static int getChildPIDs(int ppid, int *pids, int maxCount) {
    int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_ALL, 0};
    size_t size = 0;

    // Get the size needed.
    if (sysctl(mib, 3, NULL, &size, NULL, 0) < 0) {
        return 0;
    }

    struct kinfo_proc *procs = (struct kinfo_proc *)malloc(size);
    if (procs == NULL) {
        return 0;
    }

    if (sysctl(mib, 3, procs, &size, NULL, 0) < 0) {
        free(procs);
        return 0;
    }

    int count = size / sizeof(struct kinfo_proc);
    int found = 0;

    for (int i = 0; i < count && found < maxCount; i++) {
        if (procs[i].kp_eproc.e_ppid == ppid) {
            pids[found] = procs[i].kp_proc.p_pid;
            found++;
        }
    }

    free(procs);
    return found;
}

// getProcPath returns the full path for a given PID.
static int getProcPath(int pid, char *buf, int bufSize) {
    return proc_pidpath(pid, buf, bufSize);
}
*/
import "C"

import (
	"path/filepath"
	"unsafe"
)

// DetectForeground returns info about the foreground process for a given shell PID.
// It walks the process tree from the shell PID downward, finding the deepest child.
func DetectForeground(shellPID int) (*Info, error) {
	pid := shellPID

	// Walk down the process tree to find the deepest child (foreground process).
	for {
		var childPIDs [64]C.int
		count := C.getChildPIDs(C.int(pid), &childPIDs[0], 64)
		if count == 0 {
			break
		}
		// Take the first child (most recently forked tends to be first).
		pid = int(childPIDs[0])
	}

	// If we didn't move from shell, the shell itself is foreground.
	if pid == shellPID {
		return getProcessInfo(shellPID), nil
	}

	return getProcessInfo(pid), nil
}

func getProcessInfo(pid int) *Info {
	var pathBuf [C.PROC_PIDPATHINFO_MAXSIZE]C.char
	ret := C.getProcPath(C.int(pid), &pathBuf[0], C.int(len(pathBuf)))
	if ret <= 0 {
		return &Info{PID: pid}
	}

	path := C.GoString(&pathBuf[0])
	name := filepath.Base(path)

	return &Info{
		PID:  pid,
		Name: name,
		Path: path,
	}
}

// GetChildPIDs returns child PIDs for a given parent PID (exported for testing).
func GetChildPIDs(ppid int) []int {
	var childPIDs [256]C.int
	count := C.getChildPIDs(C.int(ppid), &childPIDs[0], 256)
	result := make([]int, count)
	for i := 0; i < int(count); i++ {
		result[i] = int(*(*C.int)(unsafe.Pointer(uintptr(unsafe.Pointer(&childPIDs[0])) + uintptr(i)*unsafe.Sizeof(childPIDs[0]))))
	}
	return result
}
