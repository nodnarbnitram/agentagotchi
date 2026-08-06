import Foundation

public enum AdminTransportError: Error, LocalizedError, Equatable, Sendable {
    case socketCreationFailed(Int32)
    case connectionFailed(Int32)
    case writeFailed(Int32)
    case readFailed(Int32)
    case connectionClosed
    case frameTooLarge
    case emptyFrame
    case responseTypeMismatch(expected: String, actual: String)
    case serverRejected(String)

    public var errorDescription: String? {
        switch self {
        case .socketCreationFailed, .connectionFailed, .writeFailed, .readFailed,
             .connectionClosed, .frameTooLarge, .emptyFrame:
            return "Edge not running"
        case let .responseTypeMismatch(expected, actual):
            return "unexpected administration response (expected \(expected), got \(actual))"
        case let .serverRejected(message):
            return message.isEmpty ? "Edge rejected the administration request" : message
        }
    }

    public var indicatesEdgeUnavailable: Bool {
        switch self {
        case .socketCreationFailed, .connectionFailed, .writeFailed, .readFailed,
             .connectionClosed, .frameTooLarge, .emptyFrame:
            return true
        case .responseTypeMismatch, .serverRejected:
            return false
        }
    }
}

/// A small abstraction that keeps the API layer independent of the socket.
/// Tests can provide a connected descriptor or another in-memory transport.
public protocol AdminTransport {
    func send(_ request: AdminRequest) throws -> AdminResponse
}

/// Thin, synchronous client for the Edge administration contract.
public final class EdgeAdminClient: @unchecked Sendable {
    private let transport: any AdminTransport

    public init(transport: any AdminTransport) {
        self.transport = transport
    }

    public convenience init(socketPath: String = UnixSocketTransport.defaultSocketPath) {
        self.init(transport: UnixSocketTransport(socketPath: socketPath))
    }

    public func send(_ request: AdminRequest) throws -> AdminResponse {
        let response = try transport.send(request)
        guard response.schema == AdminSchema.v1 else {
            throw AdminCodecError.schemaMismatch(expected: AdminSchema.v1, actual: response.schema)
        }
        guard response.type == request.type else {
            throw AdminTransportError.responseTypeMismatch(expected: request.type, actual: response.type)
        }
        guard response.ok else {
            throw AdminTransportError.serverRejected(response.error ?? "")
        }
        return response
    }

    @discardableResult
    public func status() throws -> AdminStatus {
        let response = try send(.status)
        guard let status = response.status else {
            throw AdminCodecError.invalidFrame("status response did not include status")
        }
        guard status.schema == AdminSchema.v1, status.type == "status" else {
            throw AdminCodecError.invalidFrame("status payload did not identify agentagotchi.admin.v1")
        }
        return status
    }

    @discardableResult
    public func beginPairing(role: PairingRole, clientName: String) throws -> PairingCode {
        let response = try send(.pairingBegin(role: role, clientName: clientName))
        guard let code = response.code else {
            throw AdminCodecError.invalidFrame("pairing_begin response did not include code")
        }
        return code
    }

    public func pendingPairings() throws -> [PairingCode] {
        try send(.pairingPending).pending ?? []
    }

    public func approve(codeID: String) throws {
        _ = try send(.pairingApprove(codeID: codeID))
    }

    public func deny(codeID: String) throws {
        _ = try send(.pairingDeny(codeID: codeID))
    }

    public func credentials() throws -> [PairingCredential] {
        try send(.pairingList).credentials ?? []
    }

    public func revoke(credentialID: String) throws {
        _ = try send(.pairingRevoke(credentialID: credentialID))
    }
}
