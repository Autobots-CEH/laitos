package api

import (
	"errors"
	"fmt"
	"github.com/HouzuoGuo/websh/bridge"
	"github.com/HouzuoGuo/websh/feature"
	"github.com/HouzuoGuo/websh/frontend/common"
	"net/http"
)

const TwilioHandlerTimeoutSec = 14 // as of 2017-02-23, the timeout is required by Twilio on both SMS and call hooks.

// Implement handler for Twilio phone number's SMS hook.
type TwilioSMSHook struct {
}

func (hand *TwilioSMSHook) MakeHandler(cmdProc *common.CommandProcessor) (http.HandlerFunc, error) {
	fun := func(w http.ResponseWriter, r *http.Request) {
		// SMS message is in "Body" parameter
		ret := cmdProc.Process(feature.Command{
			TimeoutSec: TwilioHandlerTimeoutSec,
			Content:    r.FormValue("Body"),
		})
		// In case both PIN and shortcuts mismatch, try to conceal this endpoint.
		if ret.Error == bridge.ErrPINAndShortcutNotFound {
			http.Error(w, "404 page not found", http.StatusNotFound)
		}
		// Generate normal XML response
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Cache-Control", "must-revalidate")
		w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response><Message>%s</Message></Response>
`, XMLEscape(ret.CombinedOutput))))
	}
	return fun, nil
}

// Implement handler for Twilio phone number's telephone hook.
type TwilioCallHook struct {
	CallGreeting     string // a message to speak upon picking up a call
	CallbackEndpoint string // URL (e.g. /handle_my_call) to command handler endpoint (TwilioCallCallback)
}

func (hand *TwilioCallHook) MakeHandler(_ *common.CommandProcessor) (http.HandlerFunc, error) {
	if hand.CallGreeting == "" || hand.CallbackEndpoint == "" {
		return nil, errors.New("Greeting or handler endpoint is empty")
	}
	fun := func(w http.ResponseWriter, r *http.Request) {
		// The greeting XML tells Twilio to ask user for DTMF input, and direct the input to another URL endpoint.
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Cache-Control", "must-revalidate")
		w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Gather action="%s" method="POST" timeout="30" finishOnKey="#" numDigits="1000">
        <Say>%s</Say>
    </Gather>
</Response>
`, hand.CallbackEndpoint, XMLEscape(hand.CallGreeting))))
	}
	return fun, nil
}

// Implement handler for Twilio phone number's telephone callback (triggered by response of TwilioCallHook).
type TwilioCallCallback struct {
	MyEndpoint string // URL to the callback itself
}

func (hand *TwilioCallCallback) MakeHandler(cmdProc *common.CommandProcessor) (http.HandlerFunc, error) {
	if hand.MyEndpoint == "" {
		return nil, errors.New("Handler endpoint is empty")
	}
	fun := func(w http.ResponseWriter, r *http.Request) {
		// DTMF input digits are in "Digits" parameter
		ret := cmdProc.Process(feature.Command{
			TimeoutSec: TwilioHandlerTimeoutSec,
			Content:    DTMFDecode(r.FormValue("Digits")),
		})
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Cache-Control", "must-revalidate")
		// Say sorry and hang up in case of incorrect PIN/shortcut
		if ret.Error == bridge.ErrPINAndShortcutNotFound {
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Say>Sorry</Say>
	<Hangup/>
</Response>
`))
		} else {
			// Repeat output three times and listen for the next input
			combinedOutput := XMLEscape(ret.CombinedOutput)
			w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Gather action="%s" method="POST" timeout="30" finishOnKey="#" numDigits="1000">
        <Say>%s, repeat again, %s, repeat again, %s, over.</Say>
    </Gather>
</Response>
`, hand.MyEndpoint, combinedOutput, combinedOutput, combinedOutput)))
		}
	}
	return fun, nil
}
