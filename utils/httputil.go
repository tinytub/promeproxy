package utils

import (
	"net"
	"net/http"
	"time"
)

func NewClient(rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt}
}

func NewClientForTimeOut() (*http.Client, error) {

	timeout := time.Duration(3 * time.Second)
	var rt http.RoundTripper = NewDeadlineRoundTripper(timeout)

	return NewClient(rt), nil
}

func NewDeadlineRoundTripper(timeout time.Duration) http.RoundTripper {
	return &http.Transport{
		DisableKeepAlives: true,
		Dial: func(netw, addr string) (c net.Conn, err error) {
			start := time.Now()

			c, err = net.DialTimeout(netw, addr, timeout)
			if err != nil {
				return nil, err
			}

			//TODO if timeout, set a metric to prometheus
			if err = c.SetDeadline(start.Add(timeout)); err != nil {
				c.Close()
				return nil, err
			}

			return c, nil
		},
	}
}
