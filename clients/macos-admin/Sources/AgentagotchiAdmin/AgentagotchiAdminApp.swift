import SwiftUI
import AppKit
import AdminClient

@main
struct AgentagotchiAdminApp: App {
    @StateObject private var model: AdminViewModel

    init() {
        _model = StateObject(wrappedValue: AdminViewModel())
    }

    var body: some Scene {
        WindowGroup("Agentagotchi Edge") {
            AdminDashboardView(model: model)
                .frame(minWidth: 760, minHeight: 620)
        }
    }
}

struct AdminDashboardView: View {
    @ObservedObject var model: AdminViewModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                header
                if let errorMessage = model.errorMessage {
                    ErrorBanner(message: errorMessage)
                }
                statusSection
                pairingSection
                pendingSection
                credentialsSection
            }
            .padding(28)
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear { model.startRefreshing() }
        .onDisappear { model.stopRefreshing() }
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 5) {
                Text("Agentagotchi Edge")
                    .font(.largeTitle.weight(.semibold))
                Text("Owner-only administration")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button {
                model.refresh()
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
            .disabled(model.isRefreshing)
        }
    }

    private var statusSection: some View {
        GroupBox("Status") {
            if let status = model.status {
                VStack(alignment: .leading, spacing: 14) {
                    HStack {
                        Label("Running", systemImage: "circle.fill")
                            .foregroundStyle(.green)
                        Spacer()
                        Text("Started \(status.startedAt, style: .date) at \(status.startedAt, style: .time)")
                            .foregroundStyle(.secondary)
                    }
                    StatusGrid(status: status)
                }
                .padding(8)
            } else {
                HStack(spacing: 10) {
                    Image(systemName: "bolt.horizontal.circle")
                    Text("Edge not running")
                        .foregroundStyle(.secondary)
                }
                .padding(8)
            }
        }
    }

    private var pairingSection: some View {
        GroupBox("Begin pairing") {
            HStack(alignment: .bottom, spacing: 12) {
                Picker("Role", selection: $model.pairingRole) {
                    ForEach(PairingRole.allCases) { role in
                        Text(role.displayName).tag(role)
                    }
                }
                .frame(width: 170)

                TextField("Client name", text: $model.clientName)
                    .textFieldStyle(.roundedBorder)
                    .frame(minWidth: 220)

                Button("Begin") {
                    model.beginPairing()
                }
                .keyboardShortcut(.defaultAction)
            }
            .padding(8)
            Text("The one-use code is held in memory until approval. It is not persisted or logged.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 8)
                .padding(.bottom, 8)
        }
    }

    private var pendingSection: some View {
        GroupBox("Pending pairing codes") {
            if model.pendingCodes.isEmpty {
                Text("No pending codes")
                    .foregroundStyle(.secondary)
                    .padding(8)
            } else {
                VStack(spacing: 0) {
                    ForEach(model.pendingCodes) { code in
                        PendingCodeRow(code: code, approve: model.approve, deny: model.deny)
                        if code.id != model.pendingCodes.last?.id {
                            Divider()
                        }
                    }
                }
                .padding(8)
            }
        }
        .overlay(alignment: .bottom) {
            if let secret = model.oneTimePairingSecret {
                OneTimeSecretCard(secret: secret) {
                    copyToClipboard(secret.value)
                    model.dismissOneTimeSecret()
                } dismiss: {
                    model.dismissOneTimeSecret()
                }
                .padding(12)
                .offset(y: 112)
            }
        }
        .padding(.bottom, model.oneTimePairingSecret == nil ? 0 : 112)
    }

    private var credentialsSection: some View {
        GroupBox("Credentials") {
            if model.credentials.isEmpty {
                Text("No paired credentials")
                    .foregroundStyle(.secondary)
                    .padding(8)
            } else {
                VStack(spacing: 0) {
                    ForEach(model.credentials) { credential in
                        CredentialRow(credential: credential) {
                            model.revoke(credential)
                        }
                        if credential.id != model.credentials.last?.id {
                            Divider()
                        }
                    }
                }
                .padding(8)
            }
            Text("Credential values are redacted by the Edge and are never shown by this list.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(8)
        }
    }
}

private struct StatusGrid: View {
    let status: AdminStatus

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 28, verticalSpacing: 9) {
            GridRow {
                StatusValue(label: "Role", value: status.role)
                StatusValue(label: "Version", value: status.version)
                StatusValue(label: "Aggregate", value: status.aggregateState)
            }
            GridRow {
                StatusValue(label: "Paired devices", value: "\(status.pairedDevices)")
                StatusValue(label: "Paired Edges", value: "\(status.pairedEdges)")
                StatusValue(label: "Connected peers", value: "\(status.connectedPeers)")
            }
            GridRow {
                StatusValue(label: "Task Presences", value: "\(status.taskPresences)")
                Color.clear.frame(width: 1, height: 1)
                Color.clear.frame(width: 1, height: 1)
            }
        }
    }
}

private struct StatusValue: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.body.monospacedDigit())
        }
        .frame(minWidth: 150, alignment: .leading)
    }
}

private struct PendingCodeRow: View {
    let code: PairingCode
    let approve: (PairingCode) -> Void
    let deny: (PairingCode) -> Void

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text(code.clientName.isEmpty ? "Unnamed client" : code.clientName)
                    .fontWeight(.medium)
                Text("\(code.role.displayName) · ID \(code.id)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                Text(code.approved ? "Approved" : "Awaiting approval")
                    .foregroundStyle(code.approved ? Color.green : Color.primary)
                Text("Expires \(code.expiresAt, style: .time)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Button("Approve") {
                approve(code)
            }
            .disabled(code.approved || code.consumed)
            Button("Deny", role: .destructive) {
                deny(code)
            }
            .disabled(code.consumed)
        }
        .padding(.vertical, 8)
    }
}

private struct CredentialRow: View {
    let credential: PairingCredential
    let revoke: () -> Void
    @State private var showingConfirmation = false

    var body: some View {
        HStack(spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text(credential.clientName.isEmpty ? "Unnamed client" : credential.clientName)
                    .fontWeight(.medium)
                Text("\(credential.role.displayName) · ID \(credential.id)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Text("•••••••• (redacted)")
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
            Text(credential.revoked ? "Revoked" : "Active")
                .foregroundStyle(credential.revoked ? Color.secondary : Color.green)
            if !credential.revoked {
                Button("Revoke", role: .destructive) {
                    showingConfirmation = true
                }
                .confirmationDialog(
                    "Revoke this pairing?",
                    isPresented: $showingConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Revoke", role: .destructive, action: revoke)
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text("The paired client will be disconnected and cannot reconnect with this credential.")
                }
            }
        }
        .padding(.vertical, 8)
    }
}

private struct OneTimeSecretCard: View {
    let secret: AdminViewModel.OneTimePairingSecret
    let copy: () -> Void
    let dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Label(secret.title, systemImage: "key.fill")
                    .fontWeight(.semibold)
                Spacer()
                Button("Dismiss", action: dismiss)
                    .buttonStyle(.borderless)
            }
            Text(secret.value)
                .font(.title3.monospaced())
                .textSelection(.enabled)
            HStack {
                Button("Copy", action: copy)
                Text("shown once — store it now")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text("The connecting client redeems this one-use code for its role-scoped credential.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(14)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(.tint.opacity(0.5)))
        .shadow(radius: 8)
    }
}

private struct ErrorBanner: View {
    let message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle.fill")
            .foregroundStyle(.orange)
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
    }
}

private func copyToClipboard(_ value: String) {
    #if os(macOS)
    let pasteboard = NSPasteboard.general
    pasteboard.clearContents()
    pasteboard.setString(value, forType: .string)
    #endif
}
