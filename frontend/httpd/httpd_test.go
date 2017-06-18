package httpd

import (
	"github.com/HouzuoGuo/laitos/email"
	"github.com/HouzuoGuo/laitos/frontend/common"
	"github.com/HouzuoGuo/laitos/frontend/httpd/api"
	"io/ioutil"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHTTPD_StartAndBlock(t *testing.T) {
	// Create a temporary file for index
	indexFile := "/tmp/test-laitos-index.html"
	if err := ioutil.WriteFile(indexFile, []byte("this is index #LAITOS_CLIENTADDR #LAITOS_3339TIME"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create a temporary directory of file
	htmlDir := "/tmp/test-laitos-dir"
	if err := os.MkdirAll(htmlDir, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(htmlDir)
	if err := ioutil.WriteFile(htmlDir+"/a.html", []byte("a html"), 0644); err != nil {
		t.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	daemon := HTTPD{
		ListenAddress:    "127.0.0.1",
		ListenPort:       1024 + rand.Intn(65535-1024),
		Processor:        &common.CommandProcessor{},
		ServeDirectories: map[string]string{"my/dir": "/tmp/test-laitos-dir"},
		BaseRateLimit:    10, // good enough for both HTTPD and API tests
		SpecialHandlers: map[string]api.HandlerFactory{
			"/": &api.HandleHTMLDocument{HTMLFilePath: indexFile},
		},
	}
	// Must not initialise if command processor is insane
	if err := daemon.Initialise(); err == nil || !strings.Contains(err.Error(), common.ErrBadProcessorConfig) {
		t.Fatal("did not error due to insane CommandProcessor")
	}
	daemon.Processor = common.GetTestCommandProcessor()
	// Set up API handlers
	daemon.Processor = common.GetTestCommandProcessor()
	daemon.SpecialHandlers["/info"] = &api.HandleSystemInfo{FeaturesToCheck: daemon.Processor.Features}
	daemon.SpecialHandlers["/cmd_form"] = &api.HandleCommandForm{}
	daemon.SpecialHandlers["/gitlab"] = &api.HandleGitlabBrowser{PrivateToken: "token-does-not-matter-in-this-test"}
	daemon.SpecialHandlers["/html"] = &api.HandleHTMLDocument{HTMLFilePath: indexFile}
	daemon.SpecialHandlers["/mail_me"] = &api.HandleMailMe{
		Recipients: []string{"howard@localhost"},
		Mailer: email.Mailer{
			MailFrom: "howard@localhost",
			MTAHost:  "localhost",
			MTAPort:  25,
		},
	}
	daemon.SpecialHandlers["/proxy"] = &api.HandleWebProxy{MyEndpoint: "/proxy"}
	daemon.SpecialHandlers["/sms"] = &api.HandleTwilioSMSHook{}
	daemon.SpecialHandlers["/call_greeting"] = &api.HandleTwilioCallHook{CallGreeting: "Hi there", CallbackEndpoint: "/test"}
	daemon.SpecialHandlers["/call_command"] = &api.HandleTwilioCallCallback{MyEndpoint: "/endpoint-does-not-matter-in-this-test"}
	if err := daemon.Initialise(); err != nil {
		t.Fatal(err)
	}
	// Run tests now
	TestHTTPD(&daemon, t)
	TestAPIHandlers(&daemon, t)
}
