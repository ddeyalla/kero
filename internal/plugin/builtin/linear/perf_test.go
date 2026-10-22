package linear

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"kero/internal/proto"
)

// bigState is a workspace of n issues across projects, labels and states.
func bigState(n int) *proto.State {
	types := []string{"started", "unstarted", "backlog", "completed", "completed", "canceled"}
	issues := make([]proto.LinearIssue, n)
	for i := range issues {
		issues[i] = issue(i+1, types[i%len(types)], inProject(fmt.Sprintf("Project %d", i%12)), func(is *proto.LinearIssue) {
			is.Priority = i % 5
			is.Labels = []string{"bug", "ui"}
			is.Assignee, is.AssigneeID = "Some Person", "u1"
			if i%7 == 0 && i > 0 {
				is.Parent = fmt.Sprintf("BRU-%d", i)
			}
		})
	}
	return stateOfSnap(snap(issues...), session("s1", "pr-a", "p1"))
}

// legacyHash is how the pane told a changed state before: a copy of the
// issues, a JSON encoding of it and a digest of that.
func legacyHash(st *proto.State) [sha256.Size]byte {
	s := snapshotOf(st)
	b, _ := json.Marshal(&s)
	return sha256.Sum256(b)
}

func BenchmarkAbsorbLegacy3000(b *testing.B) {
	st := bigState(3000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = legacyHash(st)
	}
}

// BenchmarkAbsorbUnchanged3000 is a broadcast that changed nothing the pane
// draws: only the fingerprint is paid.
func BenchmarkAbsorbUnchanged3000(b *testing.B) {
	st := bigState(3000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fingerprint(st)
	}
}

func BenchmarkBuildTreeList3000(b *testing.B) {
	in := input(snapshotPtr(bigState(3000)))
	in.st.Collapsed = nil
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = buildTreeOrder(in)
	}
}

func BenchmarkBuildTreeBoard3000(b *testing.B) {
	in := input(snapshotPtr(bigState(3000)))
	in.st.Mode = modeBoard
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = buildTreeOrder(in)
	}
}

func snapshotPtr(st *proto.State) *snapshot {
	s := snapshotOf(st)
	return &s
}
