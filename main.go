package main

import (
	"context"
	"crypto/tls"
	"flag"
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

	certfile, keyfile string
	dataEnd           string
)

func main() {
	var hostname, _ = os.Hostname()
	// Main parameter
	flag.StringVar(&srv.Addr, "listen", ":8025", "Address to bind to.")
	flag.StringVar(&srv.Appname, "appname", "smtpd", "Name of the service.")
	flag.StringVar(&srv.Hostname, "servername", hostname, "hostname for the service to use.")
	flag.DurationVar(&srv.Timeout, "timeout", 5*time.Minute, "Maximum wait time for all network operation")

	// TLS config
	flag.BoolVar(&srv.TLSListener, "tlsonly", false, "Start the server in smtps only work if tls material was provided.")
	flag.BoolVar(&srv.TLSRequired, "tlsrequired", false, "Enforce STARTTLS.")
	flag.StringVar(&certfile, "cert", "", "Certificate to use for TLS server.")
	flag.StringVar(&keyfile, "key", "", "Private key to use for TLS server.")

	// Util parameter
	flag.BoolVar(&smtpd.Debug, "debug", false, "Enable debug log from smtpd.")
	flag.IntVar(&srv.MaxSize, "maxsize", 0, "Maximum bytes to accept for mail data. (0 means no limit)")

	// Program customization
	flag.StringVar(&dataEnd, "dataend", "", "String to write at the end of the log after mail data.")

	flag.Parse()

	var err error
	if certfile != "" && keyfile != "" {
		err = srv.ConfigureTLS(certfile, keyfile)
		if err != nil {
			log.Fatal(err)
		}
	} else if certfile != "" || keyfile != "" {
		log.Fatal("There is a missing -cert or -key")
	}

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

	err = ListenAndServe()
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
	log.Printf("received from: %v from: %v to: %v \n%s%s", remoteAddr, from, to, data, dataEnd)
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
