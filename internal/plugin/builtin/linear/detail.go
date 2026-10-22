package linear

import (
	"net/url"
	"slices"
	"strings"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/proto"
	"kero/locales"
)

// The detail of one issue, drawn in the pane's place, with the way back:
// the title, the properties with the status to change, the sessions that
// worked on it with Resume, the description, the sub-issues, the files and
// links, the issues related, and what was said under it.

// The most rows of a kind the detail draws, and the most comments (the
// latest), so that one issue cannot use up the tree.
const (
	maxDetailRows = 60
	maxComments   = 40
)

// detail draws the open issue: its head, then the rest.
func (b *builder) detail() []*sdk.Node {
	head, body := b.detailParts(true)
	return append([]*sdk.Node{head}, body...)
}

// detailParts draws the open issue as its head, which a pane keeps fixed,
// and the rest. The issue as the board has it gives the state of the
// moment; the detail read gives the rest. notice tells whether the rest
// holds the last thing that went wrong, which a pane that shows it in
// another place leaves out.
func (b *builder) detailParts(notice bool) (head *sdk.Node, out []*sdk.Node) {
	in := b.in
	live := in.snap.issue(in.st.Open)
	d := in.detail
	var ident, link string
	switch {
	case live != nil:
		ident, link = live.Identifier, live.URL
	case d != nil:
		ident, link = d.Identifier, d.URL
	}
	parts := []*sdk.Node{
		sdk.IconButton("d-back", "back").Label(locales.LinearAllIssues()),
		caption("d-ident", ident),
		sdk.IconButton("d-copy-id", "copy").Label(locales.LinearCopyId()).Enabled(ident != ""),
		sdk.IconButton("d-copy-link", "link").Label(locales.LinearCopyLink()).Enabled(link != ""),
		sdk.Spacer("d-space"),
	}
	if live != nil {
		if i := slices.Index(b.order, live.ID); i >= 0 {
			parts = append(parts,
				caption("d-pos", locales.LinearPosition(itoa(i+1), itoa(len(b.order)))),
				sdk.IconButton("d-prev", "chevron-up").Label(locales.LinearPrevIssue()).Enabled(i > 0),
				sdk.IconButton("d-next", "chevron-down").Label(locales.LinearNextIssue()).Enabled(i < len(b.order)-1),
			)
		}
	}
	parts = append(parts, sdk.IconButton("d-ext", "external").Label(locales.LinearOpenInLinear()).Enabled(safeURL(link)))
	head = sdk.Row("d-hdr", parts...).Gap(8).Align(protocol.AlignCenter)

	if d == nil {
		if in.detailErr != "" {
			out = append(out, sdk.EmptyState("d-err", locales.LinearUnavailable(), in.detailErr).Action(locales.LinearRetry()))
		} else if live == nil {
			out = append(out, sdk.EmptyState("d-gone", locales.LinearUnavailable(), locales.LinearUnavailableDetail()).Action(locales.LinearRetry()))
		} else {
			out = append(out, txt("d-title", live.Title).Role(protocol.RoleTitle),
				caption("d-reading", locales.LinearReading()))
		}
		return head, out
	}

	cur := *d
	if live != nil {
		// The board is fresher than the detail read before it.
		cur.LinearIssue = mergeLive(d.LinearIssue, *live)
	}
	out = append(out, txt("d-title", cur.Title).Role(protocol.RoleTitle))
	if notice && in.notice != "" {
		out = append(out, sdk.Text("notice", in.notice).Role(protocol.RoleCaption).Color(protocol.ColorDanger).Lines(2))
	}
	out = append(out, b.properties(&cur), b.sessions(&cur))
	if cur.Description != "" {
		out = append(out, sdk.Markdown("d-desc", clip(markdown(cur.Description))))
	}
	out = append(out, b.subIssues(&cur)...)
	out = append(out, b.resources(&cur)...)
	out = append(out, b.related(&cur)...)
	out = append(out, b.activity(&cur)...)
	return head, out
}

// mergeLive takes the fields the board keeps current over those of the
// detail read earlier.
func mergeLive(read, live proto.LinearIssue) proto.LinearIssue {
	read.State, read.StateType = live.State, live.StateType
	read.Session, read.SessionName, read.SessionStatus, read.Sessions = live.Session, live.SessionName, live.SessionStatus, live.Sessions
	read.Priority, read.Assignee, read.AssigneeID = live.Priority, live.Assignee, live.AssigneeID
	read.Title = live.Title
	return read
}

// section is a titled group of rows: a heading with its count.
func heading(id, title string, n int) *sdk.Node {
	t := title
	if n > 0 {
		t += "  " + itoa(n)
	}
	return sdk.Text(id, t).Role(protocol.RoleStrong).Color(protocol.ColorMuted)
}

// property is a row of the properties: its name, and its value.
func property(id, name string, value *sdk.Node) *sdk.Node {
	return sdk.Row(id, caption(id+"-k", name).Width(84), value).Gap(8)
}

// properties are the issue's status, to change, and what else is known of
// it.
func (b *builder) properties(d *proto.LinearIssueDetail) *sdk.Node {
	snap := b.in.snap
	rows := []*sdk.Node{heading("p-h", locales.LinearProperties(), 0)}

	// The status is a menu of the team's states, the issue's checked. A
	// move asked for shows until the board has it.
	shown := d.State
	if b.in.moving != "" {
		shown = b.in.moving
	}
	var items []protocol.Item
	for _, s := range snap.sortedStates() {
		items = append(items, protocol.Item{ID: "s:" + slug(s.Name), Title: s.Name, Active: s.Name == shown})
	}
	if len(items) == 0 {
		items = []protocol.Item{{ID: "s:" + slug(d.State), Title: d.State, Active: true}}
	}
	rows = append(rows, property("p-status", locales.LinearStatus(),
		sdk.Menu("d-status", shown, items...).Icon(stateIcon(d.StateType))))

	if icon, color := priorityMark(d.Priority); icon != "" {
		rows = append(rows, property("p-priority", locales.LinearPriority(),
			sdk.Text("p-priority-v", priorityName(d.Priority)).Icon(icon).Color(color)))
	}
	if d.Assignee != "" {
		rows = append(rows, property("p-assignee", locales.LinearAssignee(), sdk.Text("p-assignee-v", d.Assignee).Icon("person").Lines(1)))
	}
	if d.Project != "" {
		rows = append(rows, property("p-project", locales.LinearProject(), sdk.Text("p-project-v", d.Project).Icon("folder").Lines(1)))
	}
	if d.Milestone != "" {
		rows = append(rows, property("p-milestone", locales.LinearMilestone(), sdk.Text("p-milestone-v", d.Milestone).Icon("flag").Lines(1)))
	}
	if len(d.Labels) > 0 {
		rows = append(rows, property("p-labels", locales.LinearLabels(), b.labelChips(d)))
	}
	if d.BranchName != "" {
		rows = append(rows, property("p-branch", locales.LinearBranch(), sdk.Row("p-branch-r",
			sdk.Text("p-branch-v", d.BranchName).Role(protocol.RoleMono).Lines(1).Grow(1),
			sdk.IconButton("d-copy-branch", "copy").Label(locales.LinearCopyBranch()),
		).Gap(4).Align(protocol.AlignCenter).Grow(1)))
	}
	if d.Blocked > 0 {
		rows = append(rows, property("p-blocked", locales.LinearBlockedBy(itoa(d.Blocked)), sdk.Text("p-blocked-v", "").Icon("warning").Color(protocol.ColorWarning)))
	}
	return sdk.Column("props", rows...).Surface(protocol.SurfaceCard).Padding(10).Gap(6)
}

// labelChips are the issue's labels as chips in their own colors, three to
// a row.
func (b *builder) labelChips(d *proto.LinearIssueDetail) *sdk.Node {
	const perRow = 3
	var rows []*sdk.Node
	var row []*sdk.Node
	for i, l := range d.Labels {
		if len(rows) >= 6 {
			break
		}
		row = append(row, sdk.Text("lb"+itoa(i), l).Chip(b.labelColor(l, d.LabelColors)).Lines(1).Width(110))
		if len(row) == perRow || i == len(d.Labels)-1 {
			rows = append(rows, sdk.Row("lbr"+itoa(len(rows)), row...).Gap(4).Align(protocol.AlignStart))
			row = nil
		}
	}
	return sdk.Column("p-labels-v", rows...).Gap(4).Grow(1)
}

// sessions are the conversations that worked on the issue, the latest first:
// the harness, the session it ran in and when, with Open while that session
// runs it, or Resume, which starts the conversation again where it ran. When
// no session works on the issue now there is the way to start one, or to
// link one that exists.
func (b *builder) sessions(d *proto.LinearIssueDetail) *sdk.Node {
	snap := b.in.snap
	head := heading("s-h", locales.LinearSessions(), len(d.Runs))
	rows := []*sdk.Node{head}
	if len(d.Runs) == 0 && d.Session == "" {
		rows = append(rows, caption("s-none", locales.LinearNoSession()).Lines(0))
	}
	for i, r := range d.Runs {
		id := "run" + itoa(i)
		running := r.Ended == 0 && snap.session(r.Session) != nil
		resume := proto.ResumeCommand(r.Agent, r.ID)
		color := protocol.ColorMuted
		if running {
			color = protocol.ColorSuccess
			if r.Session == d.Session {
				color = sessionColor(d.SessionStatus)
			}
		}
		top := []*sdk.Node{
			sdk.Text(id+"-a", agentTitle(r.Agent)).Icon("dot").Color(color).Role(protocol.RoleStrong).Lines(1),
			caption(id+"-n", r.SessionName).Grow(1),
		}
		switch {
		case running:
			top = append(top, sdk.Button(id+"-open", locales.LinearOpenRun()))
		case resume != nil:
			top = append(top, sdk.Button(id+"-resume", locales.LinearResume()))
		}
		if r.ID != "" {
			top = append(top, sdk.IconButton(id+"-copy", "copy").Label(locales.LinearCopyResume()))
		}
		when := locales.LinearRunStarted(proto.Ago(r.Started, b.in.now))
		if r.Ended != 0 {
			when = locales.LinearRunEnded(proto.Ago(r.Ended, b.in.now))
		}
		if via := viaWord(r.Via); via != "" {
			when += " · " + via
		}
		if r.ID != "" {
			when += " · " + r.ID
		}
		rows = append(rows,
			sdk.Row(id, top...).Gap(8).Align(protocol.AlignCenter),
			caption(id+"-w", when),
		)
	}
	var actions []*sdk.Node
	switch {
	case d.Session != "":
		if len(d.Runs) == 0 {
			actions = append(actions, sdk.Button("s-open", locales.LinearOpenSession()))
		}
		actions = append(actions, sdk.Button("s-unlink", locales.LinearUnlink()))
	default:
		if len(d.Runs) == 0 {
			actions = append(actions, sdk.Button("s-start", locales.LinearStartTask()).Primary())
		}
		var items []protocol.Item
		for _, se := range snap.Sessions {
			items = append(items, protocol.Item{ID: "ss:" + se.ID, Title: se.Name})
		}
		if len(items) > 0 {
			actions = append(actions, sdk.Menu("s-link", locales.LinearChooseSession(), capItems(items)...).Icon("link"))
		}
	}
	if len(actions) > 0 {
		rows = append(rows, sdk.Row("s-actions", actions...).Gap(8))
	}
	return sdk.Column("sessions", rows...).Surface(protocol.SurfaceCard).Padding(10).Gap(6)
}

// issueLink is a row that opens another issue.
func (b *builder) issueLink(id, prefix, ident, title, state, stateType string) *sdk.Node {
	kids := []*sdk.Node{}
	if prefix != "" {
		kids = append(kids, caption(id+"-w", prefix))
	}
	kids = append(kids,
		sdk.Text(id+"-s", "").Icon(stateIcon(stateType)).Color(stateColor(stateType)),
		caption(id+"-n", ident),
		sdk.Text(id+"-t", title).Lines(1).Grow(1),
	)
	row := sdk.Row(id, kids...).Gap(8).Align(protocol.AlignCenter).Padding(4)
	if ident != "" {
		row.Pressable().Label(ident + " " + title)
	}
	return row
}

func (b *builder) subIssues(d *proto.LinearIssueDetail) []*sdk.Node {
	var out []*sdk.Node
	if d.Parent != "" {
		title, typ := d.ParentTitle, ""
		if is := b.in.snap.issue(d.Parent); is != nil {
			title, typ = is.Title, is.StateType
		}
		out = append(out, heading("par-h", locales.LinearParent(), 0),
			b.issueLink("rel-parent", "", d.Parent, title, "", typ))
	}
	if len(d.SubIssues) > 0 {
		out = append(out, heading("sub-h", locales.LinearSubIssues(), len(d.SubIssues)))
		for _, sub := range d.SubIssues[:min(len(d.SubIssues), maxDetailRows)] {
			out = append(out, b.issueLink("sub:"+sub.ID, "", sub.Identifier, sub.Title, sub.State, sub.StateType))
		}
	}
	return out
}

// resources are the files and links on the issue, as the task's prompt.
func (b *builder) resources(d *proto.LinearIssueDetail) []*sdk.Node {
	if len(d.Attachments) == 0 {
		return nil
	}
	out := []*sdk.Node{heading("res-h", locales.LinearResources(), len(d.Attachments))}
	for i, at := range d.Attachments[:min(len(d.Attachments), maxDetailRows)] {
		id := "att" + itoa(i)
		icon := "link"
		low := strings.ToLower(at.URL)
		if strings.HasSuffix(low, ".md") || strings.Contains(strings.ToLower(at.Title), "prompt") {
			icon = "doc"
		}
		kids := []*sdk.Node{sdk.Text(id+"-t", at.Title).Icon(icon).Lines(1)}
		if at.Subtitle != "" {
			kids = append(kids, caption(id+"-d", at.Subtitle).Grow(1))
		} else {
			kids = append(kids, sdk.Spacer(id+"-g"))
		}
		kids = append(kids, sdk.Text(id+"-x", "").Icon("external").Color(protocol.ColorMuted))
		row := sdk.Row(id, kids...).Gap(8).Align(protocol.AlignCenter).Padding(4)
		if safeURL(at.URL) {
			row.Pressable().Label(at.Title)
		}
		out = append(out, row)
	}
	return out
}

// related are the issues related to this one: what blocks it, what it
// blocks, the others.
func (b *builder) related(d *proto.LinearIssueDetail) []*sdk.Node {
	if len(d.Relations) == 0 {
		return nil
	}
	out := []*sdk.Node{heading("rel-h", locales.LinearRelated(), len(d.Relations))}
	for i, r := range d.Relations[:min(len(d.Relations), maxDetailRows)] {
		out = append(out, b.issueLink("rel"+itoa(i), relationWord(r.Type), r.Identifier, r.Title, r.State, r.StateType))
	}
	return out
}

// activity is what was said under the issue: each comment a card, with who
// said it and when.
func (b *builder) activity(d *proto.LinearIssueDetail) []*sdk.Node {
	if len(d.Comments) == 0 {
		return nil
	}
	out := []*sdk.Node{sdk.Divider("act-d"), heading("act-h", locales.LinearActivity(), len(d.Comments))}
	for i, cm := range d.Comments[max(0, len(d.Comments)-maxComments):] {
		id := "cm" + itoa(i)
		author := cm.Author
		if author == "" {
			author = "Linear"
		}
		out = append(out, sdk.Column(id,
			sdk.Row(id+"-h",
				sdk.Text(id+"-a", author).Icon("person").Role(protocol.RoleStrong).Lines(1),
				caption(id+"-w", proto.Ago(cm.CreatedAt, b.in.now)),
			).Gap(8).Align(protocol.AlignCenter),
			sdk.Markdown(id+"-b", clip(markdown(cm.Body))),
		).Surface(protocol.SurfaceCard).Padding(10).Gap(6))
	}
	return out
}

// safeURL tells a web link: remote issue content may open web pages only,
// never local files or other handlers.
func safeURL(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
