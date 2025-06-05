package main

import (
	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	"github.com/tliron/glsp/server"
)

const lsName = "kro-language-server"

var version string = "0.0.1"

// main initializes and starts the Kro Language Server
// The server follows a layered architecture:
// 1. Protocol Layer - handles JSON-RPC communication
// 2. Handlers - process specific LSP method calls
// 3. Managers - coordinate business logic and state
// 4. Providers - implement specific language features
func main() {
	// Configure logging
	commonlog.Configure(int(commonlog.Info), nil)
	log := commonlog.GetLogger(lsName)
	log.Infof("Starting %s version %s", lsName, version)

	// Create our custom server instance
	kroServer := NewKroServer(log)

	// Create the LSP handler with our router
	handler := kroServer.router.CreateHandler(kroServer)

	// Create the LSP server using the glsp library
	lspServer := server.NewServer(handler, lsName, false)

	log.Info("Starting LSP Server on stdin/stdout")

	// Run the server using standard input/output
	lspServer.RunStdio()
}
