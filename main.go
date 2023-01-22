package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/mhale/smtpd"
)

var (
	srv      smtpd.Server
	ln       net.Listener
	isClosed bool

	certfile, keyfile string

	dataEnd    string // dataEnd log marker
	logQuiet   bool   // no log will be displayed
	logFull    bool   // Dump full data to log
	fileFormat string // File path to save mail data.
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
	flag.BoolVar(&logQuiet, "quiet", false, "No log will be printed.")
	flag.BoolVar(&logFull, "full", false, "Mail Data will also be printed in log.")
	flag.StringVar(&fileFormat, "fileformat", "", fileFormatHelp)

	flag.Parse()

	srv.Handler = mailProcessing

	var err error
	// certfile && keyfile check
	if certfile != "" && keyfile != "" {
		err = srv.ConfigureTLS(certfile, keyfile)
		if err != nil {
			log.Fatal(err)
		}
	} else if certfile != "" || keyfile != "" {
		log.Fatal("There is a missing -cert or -key")
	}

	// Verbosity
	var verbosityFlags int
	if smtpd.Debug {
		verbosityFlags++
	}
	if logQuiet {
		verbosityFlags++
	}
	if logFull {
		verbosityFlags++
	}
	if verbosityFlags > 1 {
		log.Print("WARNING: multiple flags present: -debug -quiet -full, unspecified behaviour")
	}

	// file Format pre processing.
	if fileFormat != "" {
		for i := 0; i < len(fileFormat); {
			j := strings.Index(fileFormat[i:], "%")
			if j == -1 || j+1 == len(fileFormat) {
				break
			}
			j += i
			switch fileFormat[j+1] {
			case 'h':
				needDataHash = true
			case 'H':
				needFullDataHash = true
			case 's':
				needTimestamp = needTimestamp | 2
			case 'N':
				needTimestamp = needTimestamp | 1
			}
			i = j + 2
		}
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

const (
	fileFormatHelp = `File path template to use when saving file data. The following replacement is done:
	- %h the sha256 hash of mail data received.
	- %H the sha256 hash of mail data received + header appended.
	- %s reception date in unix timestamp.
	- %N nanoseconds`

	logFormatHead = "remote: %v, MAIL From: <%s>, RCPT To: %v"
)

var (
	needTimestamp    uint // bitmask from low to high: nanosecond, second
	needDataHash     bool
	needFullDataHash bool

	timestampRegex    *regexp.Regexp = regexp.MustCompile("(^|[^%](%%)*)%s")
	nanosecondsRegex  *regexp.Regexp = regexp.MustCompile("(^|[^%](%%)*)%N")
	dataHashRegex     *regexp.Regexp = regexp.MustCompile("(^|[^%](%%)*)%h")
	fulldataHashRegex *regexp.Regexp = regexp.MustCompile("(^|[^%](%%)*)%H")
	percentRegex      *regexp.Regexp = regexp.MustCompile("%%")
)

// mailProcessing procresses mail according to a configuration
func mailProcessing(remoteAddr net.Addr, from string, to []string, data []byte) (err error) {
	var date time.Time
	var timestamp int64
	var nano int
	var dataChecksum []byte
	var filename string = fileFormat

	// filename treatment
	if needTimestamp > 0 {
		date = time.Now()
		timestamp = date.Unix()
		nano = date.Nanosecond()
		if needTimestamp&1 > 0 {
			filename = nanosecondsRegex.ReplaceAllString(filename, "${1}"+fmt.Sprintf("%0.9d", nano))
		}
		if needTimestamp&2 > 0 {
			filename = timestampRegex.ReplaceAllString(filename, "${1}"+fmt.Sprintf("%d", timestamp))
		}
	}
	if needDataHash {
		var payloadstart int
		for i := 0; i < 3; i++ {
			payloadstart += bytes.Index(data[payloadstart:], []byte{'\n'})
			payloadstart++
		}
		var checksum [32]byte = sha256.Sum256(data[payloadstart:])
		dataChecksum = checksum[:]
		filename = dataHashRegex.ReplaceAllString(filename, "${1}"+hex.EncodeToString(dataChecksum))
	}
	if needFullDataHash {
		var checksum [32]byte = sha256.Sum256(data)
		dataChecksum = checksum[:]
		filename = fulldataHashRegex.ReplaceAllString(filename, "${1}"+hex.EncodeToString(dataChecksum))
	}
	if filename != "" {
		filename = percentRegex.ReplaceAllString(filename, "%")
	}

	// log output
	if !logQuiet || smtpd.Debug {
		logString := fmt.Sprintf(logFormatHead, remoteAddr, from, to)
		if filename != "" {
			logString = fmt.Sprintf("%s mail data: \"%s\"", logString, filename)
		}
		if logFull {
			logString = fmt.Sprintf("%s\n%s%s", logString, data, dataEnd)
		}
		log.Print(logString)
	}

	if filename != "" {
		ferr := os.WriteFile(filename, data, 0666)
		if ferr != nil {
			log.Print(ferr)
		}
	}
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
