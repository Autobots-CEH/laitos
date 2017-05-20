package feature

import (
	"github.com/HouzuoGuo/laitos/global"
	"strings"
	"testing"
)

func TestEnvControl_Execute(t *testing.T) {
	info := EnvControl{}
	if !info.IsConfigured() {
		t.Fatal("not configured")
	}
	if err := info.Initialise(); err != nil {
		t.Fatal(err)
	}
	if err := info.SelfTest(); err != nil {
		t.Fatal(err)
	}
	if ret := info.Execute(Command{Content: "wrong"}); ret.Error != ErrBadEnvInfoChoice {
		t.Fatal(ret)
	}
	if ret := info.Execute(Command{Content: "runtime"}); ret.Error != nil || strings.Index(ret.Output, "IP") == -1 {
		t.Fatal(ret)
	}
	logger := global.Logger{}
	logger.Printf("envinfo printf test", "", nil, "")
	logger.Warningf("envinfo warningf test", "", nil, "")
	if ret := info.Execute(Command{Content: "log"}); ret.Error != nil || strings.Index(ret.Output, "envinfo printf test") == -1 {
		t.Fatal(ret)
	}
	if ret := info.Execute(Command{Content: "warn"}); ret.Error != nil || strings.Index(ret.Output, "envinfo warningf test") == -1 {
		t.Fatal(ret)
	}
	if ret := info.Execute(Command{Content: "stack"}); ret.Error != nil || strings.Index(ret.Output, "routine") == -1 {
		t.Fatal(ret)
	}
}
