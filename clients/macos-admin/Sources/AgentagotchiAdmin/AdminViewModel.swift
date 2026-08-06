import Foundation
import AdminClient

@MainActor
final class AdminViewModel: ObservableObject {
    struct OneTimePairingSecret: Identifiable {
        let id: String
        let value: String

        var title: String { "Approved pairing code" }
    }

    @Published private(set) var status: AdminStatus?
    @Published private(set) var pendingCodes: [PairingCode] = []
    @Published private(set) var credentials: [PairingCredential] = []
    @Published private(set) var edgeIsRunning = false
    @Published private(set) var isRefreshing = false
    @Published private(set) var errorMessage: String?
    @Published private(set) var oneTimePairingSecret: OneTimePairingSecret?
    @Published var pairingRole: PairingRole = .feed
    @Published var clientName = ""

    private let client: EdgeAdminClient
    private var refreshTimer: Timer?
    private var codeTokens = [String: String]()

    init(client: EdgeAdminClient = EdgeAdminClient()) {
        self.client = client
    }

    deinit {
        refreshTimer?.invalidate()
    }

    func startRefreshing() {
        guard refreshTimer == nil else { return }
        refresh()
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.refresh()
            }
        }
    }

    func stopRefreshing() {
        refreshTimer?.invalidate()
        refreshTimer = nil
    }

    func refresh() {
        guard !isRefreshing else { return }
        isRefreshing = true
        do {
            let newStatus = try client.status()
            let newPending = try client.pendingPairings()
            let newCredentials = try client.credentials()
            status = newStatus
            pendingCodes = newPending
            credentials = newCredentials
            for code in newPending {
                // Keep the short-lived code only in memory for the current
                // approval flow. It is never written to disk or logged.
                codeTokens[code.id] = code.token
            }
            edgeIsRunning = true
            errorMessage = nil
        } catch {
            edgeIsRunning = false
            errorMessage = userFacingMessage(for: error)
        }
        isRefreshing = false
    }

    func beginPairing() {
        do {
            let code = try client.beginPairing(role: pairingRole, clientName: clientName)
            codeTokens[code.id] = code.token
            clientName = ""
            edgeIsRunning = true
            errorMessage = nil
            refresh()
        } catch {
            errorMessage = userFacingMessage(for: error)
        }
    }

    func approve(_ code: PairingCode) {
        do {
            try client.approve(codeID: code.id)
            // The current Edge contract deliberately returns no long-lived
            // token from approval. It marks the short-lived code approved;
            // the connecting client redeems that code for its credential.
            // Reveal the retained one-use code once so an operator who began
            // the pairing in this app can hand it to that client.
            if let token = codeTokens[code.id] {
                oneTimePairingSecret = OneTimePairingSecret(id: code.id, value: token)
            }
            errorMessage = nil
            refresh()
        } catch {
            errorMessage = userFacingMessage(for: error)
        }
    }

    func deny(_ code: PairingCode) {
        do {
            try client.deny(codeID: code.id)
            codeTokens.removeValue(forKey: code.id)
            errorMessage = nil
            refresh()
        } catch {
            errorMessage = userFacingMessage(for: error)
        }
    }

    func revoke(_ credential: PairingCredential) {
        do {
            try client.revoke(credentialID: credential.id)
            errorMessage = nil
            refresh()
        } catch {
            errorMessage = userFacingMessage(for: error)
        }
    }

    func dismissOneTimeSecret() {
        if let id = oneTimePairingSecret?.id {
            codeTokens.removeValue(forKey: id)
        }
        oneTimePairingSecret = nil
    }

    private func userFacingMessage(for error: Error) -> String {
        if let transportError = error as? AdminTransportError,
           transportError.indicatesEdgeUnavailable {
            return "Edge not running"
        }
        if error is AdminCodecError {
            return "The Edge returned an invalid administration response."
        }
        return "The administration request could not be completed."
    }
}
