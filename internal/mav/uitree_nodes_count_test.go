package mav

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// The header's nodes= must agree with the node lines under it. This tree
// nests under AXChildren, which the printer follows, and repeats one node.
const nestedAXChildrenTree = `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","AXChildren":[
{"AXUniqueId":"HomeView","type":"Group","AXFrame":"{{0, 0}, {402, 874}}","AXChildren":[
{"AXUniqueId":"a","AXLabel":"A","type":"Button","AXFrame":"{{0, 100}, {40, 40}}"},
{"AXUniqueId":"b","AXLabel":"B","type":"Button","AXFrame":"{{0, 200}, {40, 40}}"},
{"AXUniqueId":"b","AXLabel":"B","type":"Button","AXFrame":"{{0, 200}, {40, 40}}"}]}]}]`

func TestUITreeNodesCountsThePrintedNodes(t *testing.T) {
	c, r, _, out := nativeCLI(t, "simulator")
	r.response = CommandResult{Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":false,"data":{"result":` + nestedAXChildrenTree + `}}`}
	_ = c.Run(context.Background(), []string{"ui", "tree"})
	lines := stdoutLines(out.String())
	if !strings.HasPrefix(lines[0], "ok cmd=ui.tree") {
		t.Fatal(out.String())
	}
	printed := 0
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "node ") {
			printed++
		}
	}
	if printed == 0 || !strings.Contains(lines[0], " nodes="+strconv.Itoa(printed)+" ") {
		t.Fatalf("header must count the %d printed nodes:\n%s", printed, out)
	}
}
