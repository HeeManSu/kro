import * as vscode from "vscode";
import * as fs from "fs";
import * as path from "path";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";

let client: LanguageClient;

export function activate(context: vscode.ExtensionContext) {
  // Get the server path from configuration
  const config = vscode.workspace.getConfiguration("kroLanguageServer");
  let serverPath = config.get<string>("serverPath");

  // Check if server path is provided via environment variable (for debugging)
  if (!serverPath && process.env.KRO_SERVER_PATH) {
    serverPath = process.env.KRO_SERVER_PATH;
    console.log(`Using server path from environment: ${serverPath}`);
  }

  // If no server path is configured, try to find it relative to the extension
  if (!serverPath) {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (workspaceFolder) {
      // Check if we're in the client directory (workspaceFolder ends with client)
      if (workspaceFolder.uri.fsPath.endsWith("client")) {
        // We're running from the client directory, server is at ../server/kro-lsp
        serverPath = path.join(
          workspaceFolder.uri.fsPath,
          "..",
          "server",
          "kro-lsp"
        );
      } else {
        // We're running from the project root, server is at tools/lsp/server/kro-lsp
        serverPath = vscode.Uri.joinPath(
          workspaceFolder.uri,
          "tools",
          "lsp",
          "server",
          "kro-lsp"
        ).fsPath;
      }
    }
  }

  if (!serverPath) {
    vscode.window.showErrorMessage(
      "Kro Language Server: No server path configured and no workspace folder found"
    );
    return;
  }

  // Resolve the absolute path
  serverPath = path.resolve(serverPath);

  // Check if the server binary exists
  if (!fs.existsSync(serverPath)) {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    let buildInstructions = "";

    if (workspaceFolder?.uri.fsPath.endsWith("client")) {
      // Instructions for building from client directory
      buildInstructions =
        "Please build the server first:\n" +
        "1. From this directory: cd ../../../ && go build -o tools/lsp/server/kro-lsp ./tools/lsp/server\n" +
        "2. Or use the task: Run Task > build server";
    } else {
      // Instructions for building from project root
      buildInstructions =
        "Please build the server first using 'make build/server' or 'go build -o tools/lsp/server/kro-lsp ./tools/lsp/server'";
    }

    const message = `Kro Language Server: Server binary not found at ${serverPath}. ${buildInstructions}`;
    vscode.window.showErrorMessage(message);
    console.error(message);
    return;
  }

  // Check if the server binary is executable
  try {
    fs.accessSync(serverPath, fs.constants.X_OK);
  } catch (error) {
    const message = `Kro Language Server: Server binary at ${serverPath} is not executable`;
    vscode.window.showErrorMessage(message);
    console.error(message);
    return;
  }

  console.log(`Kro Language Server: Using server at ${serverPath}`);

  // Server options - start the language server as a separate process
  const serverOptions: ServerOptions = {
    run: { command: serverPath, transport: TransportKind.stdio },
    debug: { command: serverPath, transport: TransportKind.stdio },
  };

  // Client options - configure which files the language server should handle
  const clientOptions: LanguageClientOptions = {
    // Register the server for YAML documents that might be Kro files
    documentSelector: [
      { scheme: "file", language: "yaml" },
      { scheme: "file", language: "kro-yaml" },
    ],
    synchronize: {
      // Notify the server about file changes to YAML files
      fileEvents: vscode.workspace.createFileSystemWatcher("**/*.{yaml,yml}"),
    },
    // Additional options
    outputChannelName: "Kro Language Server",
    traceOutputChannel: vscode.window.createOutputChannel(
      "Kro Language Server Trace"
    ),
  };

  // Create the language client and start it
  client = new LanguageClient(
    "kroLanguageServer",
    "Kro Language Server",
    serverOptions,
    clientOptions
  );

  // Start the client. This will also launch the server
  client
    .start()
    .then(() => {
      console.log("Kro Language Server started successfully");
      vscode.window.showInformationMessage(
        "Kro Language Server started successfully"
      );
    })
    .catch((error) => {
      console.error("Failed to start Kro Language Server:", error);
      vscode.window.showErrorMessage(
        `Failed to start Kro Language Server: ${error.message}`
      );
    });

  // Register commands
  const restartCommand = vscode.commands.registerCommand(
    "kro.restartServer",
    async () => {
      if (client) {
        await client.stop();
        await client.start();
        vscode.window.showInformationMessage("Kro Language Server restarted");
      }
    }
  );

  context.subscriptions.push(restartCommand);
}

export function deactivate(): Thenable<void> | undefined {
  if (!client) {
    return undefined;
  }
  return client.stop();
}
