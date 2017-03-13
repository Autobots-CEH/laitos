package httpclient

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Define properties for an HTTP request for DoHTTP function.
type Request struct {
	TimeoutSec  int                       // Read timeout for response (default to 30)
	Method      string                    // HTTP method (default to GET)
	Header      http.Header               // Additional request header (default to nil)
	ContentType string                    // Content type header (default to "application/x-www-form-urlencoded; charset=UTF-8")
	Body        io.Reader                 // Request body (default to nil)
	RequestFunc func(*http.Request) error // Manipulate the HTTP request at will (default to nil)
	Log         bool                      // Log request URL (default to false)
}

// Set blank attributes to their default value.
func (req *Request) FillBlanks() {
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 30
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.ContentType == "" {
		req.ContentType = "application/x-www-form-urlencoded; charset=UTF-8"
	}
}

// HTTP response as read by DoHTTP function.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// If HTTP status is not 2xx, return an error. Otherwise return nil.
func (resp *Response) Non2xxToError() error {
	if resp.StatusCode/200 != 1 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(resp.Body))
	} else {
		return nil
	}
}

// Generic function for sending an HTTP request. Placeholders in URL template must be "%s".
func DoHTTP(reqParam Request, urlTemplate string, urlValues ...interface{}) (resp Response, err error) {
	reqParam.FillBlanks()
	// Encode values in URL path
	encodedURLValues := make([]interface{}, len(urlValues))
	for i, val := range urlValues {
		encodedURLValues[i] = url.QueryEscape(fmt.Sprint(val))
	}
	fullURL := fmt.Sprintf(urlTemplate, encodedURLValues...)
	req, err := http.NewRequest(reqParam.Method, fullURL, reqParam.Body)
	if err != nil {
		return
	}
	if reqParam.Header != nil {
		req.Header = reqParam.Header
	}
	if reqParam.Log {
		log.Printf("DoHTTP: %s %s", reqParam.Method, fullURL)
	}
	// Let function to further manipulate HTTP request
	if reqParam.RequestFunc != nil {
		if err = reqParam.RequestFunc(req); err != nil {
			return
		}
	}
	req.Header.Set("Content-Type", reqParam.ContentType)
	client := &http.Client{Timeout: time.Duration(reqParam.TimeoutSec) * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	resp.Body, err = ioutil.ReadAll(response.Body)
	resp.Header = response.Header
	resp.StatusCode = response.StatusCode
	return
}