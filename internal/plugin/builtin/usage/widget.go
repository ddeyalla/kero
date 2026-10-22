package usage

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	accountusage "kero/internal/usage"
	"kero/internal/usage/cost"
	"kero/locales"
)

// layout is how much room a pane has: the short form, the large form in a
// column because it is narrow, or the large form.
type layout int

const (
	layoutLarge layout = iota
	layoutStacked
	layoutCompact
)

// layoutFor tells which form a pane of this size draws. A size of zero is
// not known yet, and draws the large form.
func layoutFor(size protocol.Size) layout {
	if size.Height > 0 && size.Height < compactHeight {
		return layoutCompact
	}
	if size.Width > 0 && size.Width < compactWidth {
		return layoutCompact
	}
	if size.Width > 0 && size.Width < stackedWidth {
		return layoutStacked
	}
	return layoutLarge
}

// The ranges of the cost card, by the key a pane saves.
const (
	rangeToday     = "today"
	rangeYesterday = "yesterday"
	range30Days    = "30d"
)

// rangeKeys are the ranges in the order the control shows them.
var rangeKeys = []string{rangeToday, rangeYesterday, range30Days}

// costRange is the cost package's range of a saved key.
func costRange(key string) cost.Range {
	switch key {
	case rangeYesterday:
		return cost.Yesterday
	case range30Days:
		return cost.Last30Days
	}
	return cost.Today
}

// validRange tells whether a key names one of the ranges.
func validRange(key string) bool { return slices.Contains(rangeKeys, key) }

func rangeTitle(key string) string {
	switch key {
	case rangeYesterday:
		return locales.UsagePluginRangeYesterday()
	case range30Days:
		return locales.UsagePluginRange30Days()
	}
	return locales.UsagePluginRangeToday()
}

// widgetTree builds what one pane draws. It is a pure function of the view,
// so a test reads the tree without a process.
func widgetTree(v *view, now time.Time) protocol.Tree {
	if layoutFor(v.size) == layoutCompact {
		return compactTree(v, now)
	}
	return largeTree(v, now)
}

// compactTree is the short form: the cost of the day in the head, then one
// row for each account, with its worst window as a bar. It has no room to
// scroll.
func compactTree(v *view, now time.Time) protocol.Tree {
	var head []*sdk.Node
	if total := totalToday(v); total != "" {
		head = append(head, sdk.Text("usage-total", locales.UsagePluginTotalToday(total)).Role(protocol.RoleStrong).Lines(1).Grow(1))
	} else {
		head = append(head, sdk.Text("usage-title", locales.UsagePluginTitle()).Role(protocol.RoleStrong).Grow(1))
	}
	if checking(v.reports) {
		head = append(head, sdk.Text("usage-checking", locales.UsagePluginChecking()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	head = append(head, sdk.IconButton("usage-refresh", "refresh").Label(locales.UsagePluginRefresh()))
	children := []*sdk.Node{sdk.Row("usage-head", head...).Gap(8).Align(protocol.AlignCenter)}
	for _, r := range v.reports {
		children = append(children, compactCard(r, now))
	}
	return sdk.Column("usage-root", children...).Gap(7).Padding(8).Tree()
}

// totalToday is the cost of today over every provider, as the short form's
// head says it, or "" when no cost has been read or nothing was spent.
func totalToday(v *view) string {
	total, estimated := totals(v.costs[cost.Today])
	if total <= 0 {
		return ""
	}
	return approx(estimated, moneyShort(total))
}

// compactCard is one account in the short form: its name, a flame when its
// worst window will run out, and what is left, over a bar of that window.
// An account with no window, such as a balance, has no bar. When a refresh
// failed, what is left is the last known one: it is muted, with a caption that
// says so, as the sidebar marks a stale ring.
func compactCard(r accountusage.Report, now time.Time) *sdk.Node {
	worst, has := worstWindow(r)
	var pace paceInfo
	if has {
		pace = windowPace(worst, now)
	}
	sum := sdk.Text("sum-"+r.ID, summary(r)).Role(protocol.RoleCaption)
	if stale(r) {
		sum.Color(protocol.ColorMuted)
	}
	top := []*sdk.Node{nameText("name-"+r.ID, r, protocol.RoleBody).Lines(1).Grow(1)}
	if has && pace.limit {
		top = append(top, sdk.Text("flame-"+r.ID, "").Icon("flame").Color(protocol.ColorDanger).Label(locales.UsagePluginLimitIn(duration(pace.hit))))
	}
	top = append(top, sum)
	children := []*sdk.Node{sdk.Row("row-"+r.ID+"-top", top...).Align(protocol.AlignSpaceBetween).Gap(6)}
	if has {
		children = append(children, windowBar("bar-"+r.ID, worst, pace, stale(r)))
	}
	if stale(r) {
		children = append(children, sdk.Text("stale-"+r.ID, locales.UsagePluginStale()).Role(protocol.RoleCaption).Color(protocol.ColorMuted).Lines(1))
	}
	return sdk.Column("row-"+r.ID, children...).Gap(3)
}

// largeTree is the full form: the cost card, then a section for each
// account, then when they were read. It scrolls.
func largeTree(v *view, now time.Time) protocol.Tree {
	children := []*sdk.Node{costCard(v)}
	for _, r := range v.reports {
		children = append(children, accountSection(v, r, now))
	}
	children = append(children, footRow(v))
	return sdk.Column("usage-root", children...).Gap(12).Padding(12).ScrollingTree()
}

// footRow is when the accounts were last read and the refresh button.
func footRow(v *view) *sdk.Node {
	var foot []*sdk.Node
	if updated := updatedLabel(v.reports, v.loading); updated != "" {
		foot = append(foot, sdk.Text("usage-updated", updated).Role(protocol.RoleCaption).Color(protocol.ColorMuted).Grow(1))
	} else {
		foot = append(foot, sdk.Spacer("usage-foot-space"))
	}
	foot = append(foot, sdk.IconButton("usage-refresh", "refresh").Label(locales.UsagePluginRefresh()))
	return sdk.Row("usage-foot", foot...).Gap(8).Align(protocol.AlignCenter)
}

// costCard is what the agents cost over the chosen range: a donut of the
// providers, and a legend that lists them, the costliest first.
func costCard(v *view) *sdk.Node {
	items := make([]protocol.Item, 0, len(rangeKeys))
	for _, key := range rangeKeys {
		items = append(items, protocol.Item{ID: key, Title: rangeTitle(key), Active: key == v.rng})
	}
	spends := shown(v.costs[costRange(v.rng)])
	total, _ := totals(spends)

	var segs []protocol.Segment
	legend := make([]*sdk.Node, 0, len(spends)+1)
	var described []string
	for _, s := range spends {
		hue := providerHue(s.Provider)
		name := providerName(v, s.Provider)
		if s.USD > 0 {
			segs = append(segs, protocol.Segment{ID: s.Provider, Label: name, Value: s.USD, Color: hue})
		}
		described = append(described, name+" "+approx(isEstimate(s), money(s.USD)))
		// The amount is at the right edge; an estimate has a muted sign
		// before it, which is a node of its own.
		amount := []*sdk.Node{sdk.Text("leg-usd-"+s.Provider, money(s.USD)).Role(protocol.RoleBody).Lines(1)}
		if isEstimate(s) {
			amount = append([]*sdk.Node{sdk.Text("leg-approx-"+s.Provider, locales.UsagePluginApprox("")).Role(protocol.RoleBody).Color(protocol.ColorMuted)}, amount...)
		}
		legend = append(legend, sdk.Row("leg-"+s.Provider,
			sdk.Text("leg-dot-"+s.Provider, "").Icon("dot").Color(hue),
			sdk.Text("leg-name-"+s.Provider, name).Role(protocol.RoleBody).Lines(1).Grow(1),
			sdk.Row("leg-amount-"+s.Provider, amount...).Gap(0),
		).Gap(6).Align(protocol.AlignCenter))
	}

	donut := sdk.Donut("usage-donut", segs...).Width(96)
	if len(spends) == 0 {
		donut.Center(locales.UsagePluginNoValue(), "")
		caption := locales.UsagePluginChecking()
		if v.costLoaded {
			caption = locales.UsagePluginNoCostLogs()
		}
		legend = append(legend, sdk.Text("usage-nocost", caption).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	} else {
		donut.Center(moneyShort(total), locales.UsagePluginCostDollars())
		donut.Label(locales.UsagePluginCostChart(rangeTitle(v.rng), strings.Join(described, ", ")))
	}

	legendColumn := sdk.Column("usage-legend", legend...).Gap(6).Grow(1)
	var body *sdk.Node
	if layoutFor(v.size) == layoutStacked {
		body = sdk.Column("usage-cost-body", donut, legendColumn).Gap(12).Align(protocol.AlignCenter)
	} else {
		body = sdk.Row("usage-cost-body", donut, legendColumn).Gap(16).Align(protocol.AlignCenter)
	}
	card := []*sdk.Node{sdk.Segmented("usage-range", items...), body}
	if models := estimatedModels(spends); len(models) > 0 {
		// One line says it; the models are for the hover and the screen reader.
		card = append(card, sdk.Text("usage-estimated", locales.UsagePluginEstimatedShort()).
			Icon("info").Role(protocol.RoleCaption).Color(protocol.ColorMuted).Lines(1).
			Label(locales.UsagePluginEstimated(strings.Join(models, ", "))))
	}
	return sdk.Column("usage-cost", card...).Surface(protocol.SurfaceCard).Padding(12).Gap(10)
}

// shown keeps the providers of a range that cost something or used tokens,
// the costliest first.
func shown(spends []cost.Spend) []cost.Spend {
	out := make([]cost.Spend, 0, len(spends))
	for _, s := range spends {
		if s.USD > 0 || s.Tokens > 0 {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].USD > out[j].USD })
	return out
}

// isEstimate tells whether a spend is an estimate: it has a model with no
// price, or one priced by a proxy.
func isEstimate(s cost.Spend) bool { return len(s.Unknown) > 0 || len(s.Estimated) > 0 }

// totals is what the providers cost together, and whether any of it is an
// estimate.
func totals(spends []cost.Spend) (total float64, estimated bool) {
	for _, s := range spends {
		total += s.USD
		estimated = estimated || isEstimate(s)
	}
	return total, estimated
}

// estimatedModels are the models whose price is unknown or a proxy, once each.
func estimatedModels(spends []cost.Spend) []string {
	var models []string
	for _, s := range spends {
		for _, m := range slices.Concat(s.Unknown, s.Estimated) {
			if !slices.Contains(models, m) {
				models = append(models, m)
			}
		}
	}
	sort.Strings(models)
	return models
}

// providerHue is the color a provider has in the donut and its legend.
func providerHue(id string) string {
	switch id {
	case "claude":
		return protocol.ColorOrange
	case "codex":
		return protocol.ColorGreen
	case "opencode":
		return protocol.ColorPurple
	case "deepseek":
		return protocol.ColorBlue
	}
	return protocol.ColorGray
}

// providerName is a provider as the app names it: from every provider it
// knows, not only the accounts set up here, because logs can show spend of a
// provider with no account. It is the id for one nothing knows.
func providerName(v *view, id string) string {
	for _, list := range [][]accountusage.Report{v.providers, v.reports} {
		for _, r := range list {
			if r.ID == id {
				return r.Name
			}
		}
	}
	return id
}

// markedText is a text with the logo of a program, if the app has one.
func markedText(id, text, program string) *sdk.Node {
	n := sdk.Text(id, text)
	if slices.Contains(protocol.KnownMarks, program) {
		n.Mark(program)
	}
	return n
}

// nameText is an account's name, with its logo when it has one.
func nameText(id string, r accountusage.Report, role string) *sdk.Node {
	return markedText(id, r.Name, r.ID).Role(role)
}

// accountSection is one account in the large form: its name, its plan and a
// button for the details, over a card with each window, the balance or what
// the account waits for, and with the details open, what it cost.
func accountSection(v *view, r accountusage.Report, now time.Time) *sdk.Node {
	open := v.open[r.ID]
	head := []*sdk.Node{nameText("name-"+r.ID, r, protocol.RoleStrong)}
	if r.Plan != "" {
		head = append(head, sdk.Text("plan-"+r.ID, planName(r.Plan)).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	icon, label := "chevron-down", locales.UsagePluginShowDetails()
	if open {
		icon, label = "chevron-up", locales.UsagePluginHideDetails()
	}
	head = append(head, sdk.Spacer("space-"+r.ID), sdk.IconButton("toggle-"+r.ID, icon).Label(label))

	var card []*sdk.Node
	for i, w := range r.Windows {
		card = append(card, windowRow(r, i, w, stale(r), now))
	}
	if r.Balance != "" {
		card = append(card, sdk.Text("balance-"+r.ID, locales.UsagePluginBalance(r.Balance)).Role(protocol.RoleBody))
	}
	if r.Error != "" {
		card = append(card, sdk.Text("error-"+r.ID, r.Error).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	} else if r.Checked.IsZero() {
		card = append(card, sdk.Text("wait-"+r.ID, locales.UsagePluginConnect()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	if stale(r) {
		card = append(card, sdk.Text("stale-"+r.ID, locales.UsagePluginStale()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	if help := helpFor(r.ID); help != "" && (r.Checked.IsZero() || r.Error != "") {
		card = append(card, sdk.Text("help-"+r.ID, help).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	if open {
		card = append(card,
			costLine("cost-today-"+r.ID, locales.UsagePluginToday(), spendOf(v.costs[cost.Today], r.ID)),
			costLine("cost-30d-"+r.ID, locales.UsagePluginLast30Days(), spendOf(v.costs[cost.Last30Days], r.ID)),
		)
	}
	return sdk.Column("sec-"+r.ID,
		sdk.Row("head-"+r.ID, head...).Gap(8).Align(protocol.AlignCenter),
		sdk.Column("card-"+r.ID, card...).Surface(protocol.SurfaceCard).Padding(12).Gap(12),
	).Gap(6)
}

// windowRow is one window of an account: its name and, when the pace says
// the limit will be reached, when; the bar with the pace marker; and what is
// left and when it resets.
func windowRow(r accountusage.Report, i int, w accountusage.Window, old bool, now time.Time) *sdk.Node {
	id := r.ID + "-" + strconv.Itoa(i)
	pace := windowPace(w, now)
	head := []*sdk.Node{sdk.Text("wlabel-"+id, windowName(w.Label)).Role(protocol.RoleStrong)}
	if pace.limit {
		head = append(head, sdk.Text("pace-"+id, locales.UsagePluginLimitIn(duration(pace.hit))).
			Icon("flame").Role(protocol.RoleCaption).Color(protocol.ColorDanger))
	}
	foot := []*sdk.Node{sdk.Text("wleft-"+id, locales.UsagePluginLeft(percent(100-clamp(w.Used)))).Role(protocol.RoleCaption).Color(protocol.ColorMuted)}
	if reset := resetLabel(w.Resets, now); reset != "" {
		foot = append(foot, sdk.Text("wreset-"+id, reset).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	return sdk.Column("win-"+id,
		sdk.Row("win-head-"+id, head...).Align(protocol.AlignSpaceBetween).Gap(8),
		windowBar("bar-"+id, w, pace, old),
		sdk.Row("win-foot-"+id, foot...).Align(protocol.AlignSpaceBetween).Gap(8),
	).Gap(4)
}

// windowBar is a window's used share as a bar. The marker is where the use
// should be by now; the bar is red once the window is nearly spent or will
// be, and muted when it is the last known one.
func windowBar(id string, w accountusage.Window, pace paceInfo, old bool) *sdk.Node {
	color := tone(w, pace)
	if old {
		color = protocol.ColorMuted
	}
	bar := sdk.Progress(id, clamp(w.Used)/100).Color(color).
		Label(locales.UsagePluginWindowBar(windowName(w.Label), locales.UsagePluginLeft(percent(100-clamp(w.Used)))))
	if pace.marker {
		bar.Marker(pace.expected)
	}
	return bar
}

// costLine is what one provider cost over a range: the range and the cost
// with the tokens, or a note when the logs say nothing.
func costLine(id, label string, s *cost.Spend) *sdk.Node {
	value := sdk.Text(id+"-value", locales.UsagePluginNoData()).Role(protocol.RoleCaption).Color(protocol.ColorMuted)
	if s != nil && (s.USD > 0 || s.Tokens > 0) {
		text := locales.UsagePluginCostTokens(approx(isEstimate(*s), moneyShort(s.USD)), tokens(s.Tokens))
		value = sdk.Text(id+"-value", text).Role(protocol.RoleCaption)
	}
	return sdk.Row(id, sdk.Text(id+"-label", label).Role(protocol.RoleStrong), value).
		Align(protocol.AlignSpaceBetween).Gap(8)
}

// spendOf is one provider's spend in a range, nil when it has none.
func spendOf(spends []cost.Spend, id string) *cost.Spend {
	for i := range spends {
		if spends[i].Provider == id {
			return &spends[i]
		}
	}
	return nil
}

// stale tells whether an account shows its last successful reading because
// the latest refresh failed.
func stale(r accountusage.Report) bool {
	return r.Error != "" && !r.Checked.IsZero()
}

// worstWindow is the window of an account that is closest to its limit, as
// the short form's bar shows it.
func worstWindow(r accountusage.Report) (accountusage.Window, bool) {
	if len(r.Windows) == 0 {
		return accountusage.Window{}, false
	}
	worst := r.Windows[0]
	for _, w := range r.Windows[1:] {
		if w.Used > worst.Used {
			worst = w
		}
	}
	return worst, true
}

// paceInfo is how a window is being used against its length.
type paceInfo struct {
	// marker tells that the window has a length, and expected is the share of
	// it that has passed.
	marker   bool
	expected float64
	// limit tells that the use so far would reach the limit before the
	// window resets, in hit from now.
	limit bool
	hit   time.Duration
}

// tone is the color of a window's bar: red when 15% or less is left or the
// limit is on its way, blue otherwise.
func tone(w accountusage.Window, pace paceInfo) string {
	if pace.limit || 100-clamp(w.Used) <= 15 {
		return protocol.ColorDanger
	}
	return protocol.ColorBlue
}

// windowLength is how long a window lasts, read from its label: five hours,
// a week, or a month back from its reset. Zero is not known, as for an
// allowance with no length.
func windowLength(w accountusage.Window) time.Duration {
	switch {
	case w.Resets.IsZero():
		return 0
	case w.Label == "5 hours":
		return 5 * time.Hour
	case strings.HasPrefix(w.Label, "Weekly"):
		return 7 * 24 * time.Hour
	case w.Label == "Monthly":
		return w.Resets.Sub(w.Resets.AddDate(0, -1, 0))
	}
	return 0
}

// windowPace tells where a window should be by now, and whether its use so
// far would end it early. The projection waits until a tenth of the window,
// and at least a quarter of an hour, has passed and 5% is used, because
// before that the rate says little.
func windowPace(w accountusage.Window, now time.Time) paceInfo {
	length := windowLength(w)
	if length <= 0 {
		return paceInfo{}
	}
	elapsed := now.Sub(w.Resets.Add(-length))
	if elapsed < 0 {
		return paceInfo{}
	}
	p := paceInfo{marker: true, expected: min(float64(elapsed)/float64(length), 1)}
	used := clamp(w.Used)
	if elapsed >= max(length/10, 15*time.Minute) && used >= 5 && used < 100 {
		hit := time.Duration(float64(elapsed) * (100 - used) / used)
		if now.Add(hit).Before(w.Resets) {
			p.limit, p.hit = true, hit
		}
	}
	return p
}

// clamp keeps a used percent between 0 and 100.
func clamp(used float64) float64 {
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}

// percent is a percent as the card says it, without a decimal.
func percent(value float64) string {
	return strconv.FormatFloat(clamp(value), 'f', 0, 64)
}

// summary is an account's one-line state in the short form: its balance,
// what is left of its worst window, or what it waits for.
func summary(r accountusage.Report) string {
	if r.Balance != "" {
		return r.Balance
	}
	if worst, ok := worstWindow(r); ok && !r.Checked.IsZero() {
		return locales.UsagePluginLeft(percent(100 - clamp(worst.Used)))
	}
	switch {
	case r.Error == "Not connected", r.Error == "Sign in again":
		return locales.UsagePluginConnect()
	case r.Error != "":
		return locales.UsagePluginUnavailable()
	}
	if r.Checked.IsZero() {
		return locales.UsagePluginChecking()
	}
	return locales.UsagePluginConnect()
}

// checking tells whether an account is still waiting for its first reading:
// it has no error to explain the wait. The short form's head says so.
func checking(reports []accountusage.Report) bool {
	for _, r := range reports {
		if r.Checked.IsZero() && r.Error == "" {
			return true
		}
	}
	return false
}

// helpFor is what a provider's card says when it has no reading yet or the
// reading failed: how to sign in, as the Usage tab says it.
func helpFor(id string) string {
	switch id {
	case "claude":
		return locales.UsagePluginHelpClaude()
	case "codex":
		return locales.UsagePluginHelpCodex()
	case "opencode":
		return locales.UsagePluginHelpOpencode()
	case "deepseek":
		return locales.UsagePluginHelpDeepseek()
	}
	return ""
}

// updatedLabel is when the accounts were last read, for the large form's
// foot.
func updatedLabel(reports []accountusage.Report, loading bool) string {
	var newest time.Time
	for _, r := range reports {
		if r.Checked.After(newest) {
			newest = r.Checked
		}
	}
	if newest.IsZero() {
		if loading {
			return locales.UsagePluginChecking()
		}
		return ""
	}
	return locales.UsagePluginUpdated(newest.Local().Format(locales.UsagePluginTimeLayout()))
}

// windowName is a window as the card says it: the five hours are a session.
func windowName(label string) string {
	switch label {
	case "5 hours":
		return locales.UsagePluginSession()
	case "Weekly":
		return locales.UsagePluginWeekly()
	case "Monthly":
		return locales.UsagePluginMonthly()
	}
	return label
}

// planName is a plan's name as the card shows it: the providers' own are
// lowercase, which reads as a word, so it is capitalized a word at a time.
func planName(plan string) string {
	words := strings.Fields(strings.ReplaceAll(plan, "_", " "))
	for i, w := range words {
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(words, " ")
}

// resetLabel is a window's reset as the card says it: the time left, or that
// it is due. A window with no reset time says nothing.
func resetLabel(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if !at.After(now) {
		return locales.UsagePluginResetDue()
	}
	return locales.UsagePluginResetsIn(duration(at.Sub(now)))
}

// duration is a time span in its two largest units: 1d 4h, 4h 12m, 12m. The
// units are those of the ages in the sidebar.
func duration(d time.Duration) string {
	pair := func(major, minor string, rest int) string {
		if rest == 0 {
			return major
		}
		return locales.UsagePluginDurationPair(major, minor)
	}
	switch {
	case d >= 24*time.Hour:
		hours := int(d % (24 * time.Hour) / time.Hour)
		return pair(locales.AgoDays(int(d/(24*time.Hour))), locales.AgoHours(hours), hours)
	case d >= time.Hour:
		minutes := int(d % time.Hour / time.Minute)
		return pair(locales.AgoHours(int(d/time.Hour)), locales.AgoMinutes(minutes), minutes)
	case d >= time.Minute:
		return locales.AgoMinutes(int(d / time.Minute))
	}
	return locales.UsagePluginUnderMinute()
}

// approx marks an amount that is an estimate.
func approx(estimated bool, text string) string {
	if estimated {
		return locales.UsagePluginApprox(text)
	}
	return text
}

// money is a cost to the cent with thousands apart, as the legend says it.
func money(usd float64) string {
	whole, frac, _ := strings.Cut(strconv.FormatFloat(usd, 'f', 2, 64), ".")
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	return "$" + whole + "." + frac
}

// moneyShort is a cost in the little room of the donut's middle: thousands
// and millions are shortened. The figure is rounded before its unit is
// chosen, so that $999,960 is $1M and not $1000K.
func moneyShort(usd float64) string {
	if usd >= 1000 {
		return "$" + shortScale(usd, []string{"K", "M"}, 1e3)
	}
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}

// tokens is a token count shortened: 950K, 22.1M, 1.1B.
func tokens(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	return shortScale(float64(n), []string{"K", "M", "B"}, 1e3)
}

// shortScale writes f in the first of units that holds it as a figure below
// a thousand, with one decimal. The figure is rounded to that decimal first,
// and a figure that rounds up to a thousand moves to the next unit.
func shortScale(f float64, units []string, step float64) string {
	scale := step
	for i, unit := range units {
		if i == len(units)-1 || math.Round(f/scale*10)/10 < step {
			return scaled(f/scale) + unit
		}
		scale *= step
	}
	return scaled(f)
}

// scaled is a number with one decimal, without a trailing ".0".
func scaled(f float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(f, 'f', 1, 64), ".0")
}

// filterReports keeps the accounts that are set up on this Mac. An empty
// installed list means they could not be told apart, so every provider is
// shown.
func filterReports(reports []accountusage.Report, installed []string) []accountusage.Report {
	if len(installed) == 0 {
		return reports
	}
	keep := make(map[string]bool, len(installed))
	for _, id := range installed {
		keep[id] = true
	}
	out := make([]accountusage.Report, 0, len(reports))
	for _, r := range reports {
		if keep[r.ID] {
			out = append(out, r)
		}
	}
	return out
}
