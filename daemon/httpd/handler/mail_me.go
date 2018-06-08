package handler

import (
	"errors"
	"fmt"
	"github.com/HouzuoGuo/laitos/daemon/common"
	"github.com/HouzuoGuo/laitos/inet"
	"github.com/HouzuoGuo/laitos/misc"
	"net/http"
)

const HandleMailMePage = `<html>
<head>
    <meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
    <title>给厚佐写信</title>
    <style>
    	textarea {
    		font-size: 20px;
    		font-weight: bold;
    	}
    	p {
    		font-size: 20px;
    		font-weight: bold;
    	}
    	input {
    		font-size: 20px;
    		font-weight: bold;
    	}
    </style>
</head>
<body>
    <form action="%s" method="post">
        <p><textarea name="msg" cols="30" rows="4"></textarea></p>
        <p><input type="submit" value="发出去"/></p>
        <p>%s</p>
    </form>
</body>
</html>
` // Mail Me page content

// Send Howard an email in a simple web form. The text on the page is deliberately written in Chinese.
type HandleMailMe struct {
	Recipients []string        `json:"Recipients"` // Recipients of these mail messages
	MailClient inet.MailClient `json:"-"`

	logger misc.Logger
}

func (mm *HandleMailMe) Initialise(logger misc.Logger, _ *common.CommandProcessor) error {
	mm.logger = logger
	if mm.Recipients == nil || len(mm.Recipients) == 0 || !mm.MailClient.IsConfigured() {
		return errors.New("HandleMailMe.Initialise: recipient list is empty or mailer is not configured")
	}
	return nil
}

func (mm *HandleMailMe) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	NoCache(w)
	if r.Method == http.MethodGet {
		// Render the page
		w.Write([]byte(fmt.Sprintf(HandleMailMePage, r.RequestURI, "")))
	} else if r.Method == http.MethodPost {
		// Retrieve message and deliver it
		if msg := r.FormValue("msg"); msg == "" {
			w.Write([]byte(fmt.Sprintf(HandleMailMePage, r.RequestURI, "")))
		} else {
			prompt := "出问题了，发不出去。"
			if err := mm.MailClient.Send(inet.OutgoingMailSubjectKeyword+"-mailme", msg, mm.Recipients...); err == nil {
				prompt = "发出去了。可以接着写。"
			} else {
				mm.logger.Warning("HandleMailMe", r.RemoteAddr, err, "failed to deliver mail")
			}
			w.Write([]byte(fmt.Sprintf(HandleMailMePage, r.RequestURI, prompt)))
		}
	}
}

func (mm *HandleMailMe) GetRateLimitFactor() int {
	return 1
}

func (mm *HandleMailMe) SelfTest() error {
	if err := mm.MailClient.SelfTest(); err != nil {
		return fmt.Errorf("HandleMailMe encountered a mail client error - %v", err)
	}
	return nil
}
