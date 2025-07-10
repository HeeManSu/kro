package main

import (
	"os"

	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	"github.com/tliron/glsp/server"
)

var (
	version = "0.0.1"
	lsName  = "kro-language-server"
)

func main() {

	commonlog.Configure(int(commonlog.Info), nil)
	log := commonlog.GetLogger(lsName)

	log.Infof("Starting %s version %s", lsName, version)

	kroServer := NewKroServer(log)
	handler := kroServer.router.createHandler()

	lspServer := server.NewServer(handler, lsName, false)

	if err := lspServer.RunStdio(); err != nil {
		log.Errorf("Error running LSP server: %v", err)
		os.Exit(1)
	}

}
