package importer

import (
	"testing"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
)

func TestSharedProbeRuntimeBuildsDirectOutbound(t *testing.T) {
	runtime, err := newSharedProbeRuntime()
	if err != nil {
		t.Fatalf("newSharedProbeRuntime() error = %v", err)
	}
	defer runtime.Close()

	outbound, err := runtime.Build(option.Outbound{
		Type:    C.TypeDirect,
		Tag:     "probe-runtime-test",
		Options: &option.DirectOutboundOptions{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if outbound == nil {
		t.Fatal("Build() returned nil outbound")
	}
	if err := common.Close(outbound); err != nil {
		t.Fatalf("Close(outbound) error = %v", err)
	}
	for _, stage := range adapter.ListStartStages {
		if stage == adapter.StartStateStart {
			return
		}
	}
	t.Fatal("sing-box start stages do not include start")
}
