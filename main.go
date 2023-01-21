package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/mhale/smtpd"
)

var (
	srv      smtpd.Server
	ln       net.Listener
	isClosed bool
)

func main() {

	go func() {
		var c = make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)

		// Wait for signal.
		<-c
		log.Println("Signal received: shutting down.")
		err := srv.Close()
		if err != nil {
			log.Println(err)
		}
		isClosed = true
		ln.Close()
		log.Println("server closed.")
	}()

	srv.Handler = logIncoming

	srv.Addr = "localhost:8025"

	err := ListenAndServe()
	if isClosed {
		err = srv.Shutdown(context.TODO())
		if err != nil {
			log.Println(err)
		}
		log.Println("server shut downed.")
	} else if err != nil {
		log.Println(err)
	}
}

func logIncoming(remoteAddr net.Addr, from string, to []string, data []byte) (err error) {
	log.Printf("received from: %v from: %v to: %v \n%s\n", remoteAddr, from, to, data)
	return
}

// ListenAndServe implemented and copied from smtpd to handle graceful shutdown.
// Small fix in vendor in Shutdown (delete default, which speed up the loop...)
func ListenAndServe() error {

	if srv.Addr == "" {
		srv.Addr = ":25"
	}
	if srv.Appname == "" {
		srv.Appname = "smtpd"
	}
	if srv.Hostname == "" {
		srv.Hostname, _ = os.Hostname()
	}
	if srv.Timeout == 0 {
		srv.Timeout = 5 * time.Minute
	}

	var err error

	// If TLSListener is enabled, listen for TLS connections only.
	if srv.TLSConfig != nil && srv.TLSListener {
		ln, err = tls.Listen("tcp", srv.Addr, srv.TLSConfig)
	} else {
		ln, err = net.Listen("tcp", srv.Addr)
	}
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}
