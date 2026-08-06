import Foundation

/// The schema used by the Edge's owner-only administration socket.
public enum AdminSchema {
    public static let v1 = "agentagotchi.admin.v1"
}

/// Roles that can be granted by the Pairing Ceremony. `admin` is deliberately
/// absent: pairing credentials are never administrator credentials.
public enum PairingRole: String, Codable, CaseIterable, Identifiable, Sendable {
    case feed
    case edgeIngress = "edge-ingress"

    public var id: String { rawValue }

    public var displayName: String {
        switch self {
        case .feed:
            return "Feed"
        case .edgeIngress:
            return "Edge ingress"
        }
    }
}

/// The seven request shapes implemented by `internal/edge/admin.go`.
public enum AdminRequest: Codable, Equatable, Sendable {
    case status
    case pairingBegin(role: PairingRole, clientName: String)
    case pairingPending
    case pairingApprove(codeID: String)
    case pairingDeny(codeID: String)
    case pairingList
    case pairingRevoke(credentialID: String)

    public var type: String {
        switch self {
        case .status:
            return "status"
        case .pairingBegin:
            return "pairing_begin"
        case .pairingPending:
            return "pairing_pending"
        case .pairingApprove:
            return "pairing_approve"
        case .pairingDeny:
            return "pairing_deny"
        case .pairingList:
            return "pairing_list"
        case .pairingRevoke:
            return "pairing_revoke"
        }
    }

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case schema
        case type
        case codeID = "codeId"
        case credentialID = "credentialId"
        case role
        case clientName
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(AdminSchema.v1, forKey: .schema)
        try container.encode(type, forKey: .type)

        switch self {
        case .status, .pairingPending, .pairingList:
            break
        case let .pairingBegin(role, clientName):
            try container.encode(role, forKey: .role)
            try container.encode(clientName, forKey: .clientName)
        case let .pairingApprove(codeID), let .pairingDeny(codeID):
            try container.encode(codeID, forKey: .codeID)
        case let .pairingRevoke(credentialID):
            try container.encode(credentialID, forKey: .credentialID)
        }
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try rejectUnknownKeys(in: container, allowed: Set(CodingKeys.allCases.map(\.stringValue)))

        let schema = try container.decode(String.self, forKey: .schema)
        guard schema == AdminSchema.v1 else {
            throw AdminCodecError.schemaMismatch(expected: AdminSchema.v1, actual: schema)
        }
        let type = try container.decode(String.self, forKey: .type)
        switch type {
        case "status":
            try requireOnlyKeys(container, [.schema, .type])
            self = .status
        case "pairing_begin":
            try requireOnlyKeys(container, [.schema, .type, .role, .clientName])
            self = try .pairingBegin(
                role: container.decode(PairingRole.self, forKey: .role),
                clientName: container.decode(String.self, forKey: .clientName)
            )
        case "pairing_pending":
            try requireOnlyKeys(container, [.schema, .type])
            self = .pairingPending
        case "pairing_approve":
            try requireOnlyKeys(container, [.schema, .type, .codeID])
            self = try .pairingApprove(codeID: container.decode(String.self, forKey: .codeID))
        case "pairing_deny":
            try requireOnlyKeys(container, [.schema, .type, .codeID])
            self = try .pairingDeny(codeID: container.decode(String.self, forKey: .codeID))
        case "pairing_list":
            try requireOnlyKeys(container, [.schema, .type])
            self = .pairingList
        case "pairing_revoke":
            try requireOnlyKeys(container, [.schema, .type, .credentialID])
            self = try .pairingRevoke(credentialID: container.decode(String.self, forKey: .credentialID))
        default:
            throw AdminCodecError.unknownRequestType(type)
        }
    }
}

/// Status returned by the Edge administration service. It contains only
/// privacy-safe counts, state, connectivity, and timestamps.
public struct AdminStatus: Codable, Equatable, Sendable {
    public let schema: String
    public let type: String
    public let role: String
    public let version: String
    public let startedAt: Date
    public let pairedDevices: Int
    public let pairedEdges: Int
    public let connectedPeers: Int
    public let taskPresences: Int
    public let aggregateState: String

    public init(
        schema: String = AdminSchema.v1,
        type: String = "status",
        role: String,
        version: String,
        startedAt: Date,
        pairedDevices: Int,
        pairedEdges: Int = 0,
        connectedPeers: Int,
        taskPresences: Int,
        aggregateState: String
    ) {
        self.schema = schema
        self.type = type
        self.role = role
        self.version = version
        self.startedAt = startedAt
        self.pairedDevices = pairedDevices
        self.pairedEdges = pairedEdges
        self.connectedPeers = connectedPeers
        self.taskPresences = taskPresences
        self.aggregateState = aggregateState
    }

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case schema, type, role, version, startedAt, pairedDevices, pairedEdges
        case connectedPeers, taskPresences, aggregateState
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try rejectUnknownKeys(in: container, allowed: Set(CodingKeys.allCases.map(\.stringValue)))
        self.schema = try container.decode(String.self, forKey: .schema)
        self.type = try container.decode(String.self, forKey: .type)
        self.role = try container.decode(String.self, forKey: .role)
        self.version = try container.decode(String.self, forKey: .version)
        self.startedAt = try container.decode(Date.self, forKey: .startedAt)
        self.pairedDevices = try container.decode(Int.self, forKey: .pairedDevices)
        self.pairedEdges = try container.decodeIfPresent(Int.self, forKey: .pairedEdges) ?? 0
        self.connectedPeers = try container.decode(Int.self, forKey: .connectedPeers)
        self.taskPresences = try container.decode(Int.self, forKey: .taskPresences)
        self.aggregateState = try container.decode(String.self, forKey: .aggregateState)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(schema, forKey: .schema)
        try container.encode(type, forKey: .type)
        try container.encode(role, forKey: .role)
        try container.encode(version, forKey: .version)
        try container.encode(startedAt, forKey: .startedAt)
        try container.encode(pairedDevices, forKey: .pairedDevices)
        if pairedEdges != 0 {
            try container.encode(pairedEdges, forKey: .pairedEdges)
        }
        try container.encode(connectedPeers, forKey: .connectedPeers)
        try container.encode(taskPresences, forKey: .taskPresences)
        try container.encode(aggregateState, forKey: .aggregateState)
    }
}

/// A short-lived pending pairing code. The token is intentionally present
/// only in the pairing flow; it is never a credential-list or status value.
public struct PairingCode: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let token: String
    public let role: PairingRole
    public let clientName: String
    public let createdAt: Date
    public let expiresAt: Date
    public let consumed: Bool
    public let approved: Bool

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case id, token, role, clientName, createdAt, expiresAt, consumed, approved
    }

    public init(
        id: String,
        token: String,
        role: PairingRole,
        clientName: String,
        createdAt: Date,
        expiresAt: Date,
        consumed: Bool = false,
        approved: Bool = false
    ) {
        self.id = id
        self.token = token
        self.role = role
        self.clientName = clientName
        self.createdAt = createdAt
        self.expiresAt = expiresAt
        self.consumed = consumed
        self.approved = approved
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try rejectUnknownKeys(in: container, allowed: Set(CodingKeys.allCases.map(\.stringValue)))
        self.id = try container.decode(String.self, forKey: .id)
        self.token = try container.decode(String.self, forKey: .token)
        self.role = try container.decode(PairingRole.self, forKey: .role)
        self.clientName = try container.decode(String.self, forKey: .clientName)
        self.createdAt = try container.decode(Date.self, forKey: .createdAt)
        self.expiresAt = try container.decode(Date.self, forKey: .expiresAt)
        self.consumed = try container.decode(Bool.self, forKey: .consumed)
        self.approved = try container.decode(Bool.self, forKey: .approved)
    }
}

/// Credential metadata returned by `pairing_list`. The Edge redacts the token
/// by omitting it; clients must never treat a list response as a secret source.
public struct PairingCredential: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let token: String?
    public let role: PairingRole
    public let clientName: String
    public let issuedAt: Date
    public let revoked: Bool

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case id, token, role, clientName, issuedAt, revoked
    }

    public init(
        id: String,
        token: String? = nil,
        role: PairingRole,
        clientName: String,
        issuedAt: Date,
        revoked: Bool
    ) {
        self.id = id
        self.token = token
        self.role = role
        self.clientName = clientName
        self.issuedAt = issuedAt
        self.revoked = revoked
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try rejectUnknownKeys(in: container, allowed: Set(CodingKeys.allCases.map(\.stringValue)))
        self.id = try container.decode(String.self, forKey: .id)
        self.token = try container.decodeIfPresent(String.self, forKey: .token)
        self.role = try container.decode(PairingRole.self, forKey: .role)
        self.clientName = try container.decode(String.self, forKey: .clientName)
        self.issuedAt = try container.decode(Date.self, forKey: .issuedAt)
        self.revoked = try container.decode(Bool.self, forKey: .revoked)
    }
}

/// The response envelope emitted by `internal/edge/admin.go`.
public struct AdminResponse: Codable, Equatable, Sendable {
    public let schema: String
    public let type: String
    public let ok: Bool
    public let error: String?
    public let status: AdminStatus?
    public let pending: [PairingCode]?
    public let credentials: [PairingCredential]?
    public let code: PairingCode?

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case schema, type, ok, error, status, pending, credentials, code
    }

    public init(
        schema: String = AdminSchema.v1,
        type: String,
        ok: Bool,
        error: String? = nil,
        status: AdminStatus? = nil,
        pending: [PairingCode]? = nil,
        credentials: [PairingCredential]? = nil,
        code: PairingCode? = nil
    ) {
        self.schema = schema
        self.type = type
        self.ok = ok
        self.error = error
        self.status = status
        self.pending = pending
        self.credentials = credentials
        self.code = code
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try rejectUnknownKeys(in: container, allowed: Set(CodingKeys.allCases.map(\.stringValue)))
        self.schema = try container.decode(String.self, forKey: .schema)
        self.type = try container.decode(String.self, forKey: .type)
        self.ok = try container.decode(Bool.self, forKey: .ok)
        self.error = try container.decodeIfPresent(String.self, forKey: .error)
        self.status = try container.decodeIfPresent(AdminStatus.self, forKey: .status)
        self.pending = try container.decodeIfPresent([PairingCode].self, forKey: .pending)
        self.credentials = try container.decodeIfPresent([PairingCredential].self, forKey: .credentials)
        self.code = try container.decodeIfPresent(PairingCode.self, forKey: .code)
    }
}

public enum AdminCodecError: Error, LocalizedError, Equatable, Sendable {
    case schemaMismatch(expected: String, actual: String)
    case unknownRequestType(String)
    case unexpectedKeys(Set<String>)
    case missingKeys(Set<String>)
    case invalidFrame(String)

    public var errorDescription: String? {
        switch self {
        case let .schemaMismatch(expected, actual):
            return "schema mismatch (expected \(expected), got \(actual))"
        case let .unknownRequestType(type):
            return "unknown administration request type: \(type)"
        case let .unexpectedKeys(keys):
            return "unexpected administration fields: \(keys.sorted().joined(separator: ", "))"
        case let .missingKeys(keys):
            return "missing administration fields: \(keys.sorted().joined(separator: ", "))"
        case let .invalidFrame(message):
            return message
        }
    }
}

/// JSON coding configured for Go's RFC3339 `time.Time` values and the JSONL
/// administration frame format.
public enum AdminCodec {
    public static let maxFrameBytes = 64 * 1024

    public static func encoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(format(date))
        }
        return encoder
    }

    public static func decoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let value = try container.decode(String.self)
            guard let date = parseDate(value) else {
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription: "invalid RFC3339 timestamp"
                )
            }
            return date
        }
        return decoder
    }

    public static func encode<T: Encodable>(_ value: T) throws -> Data {
        try encoder().encode(value)
    }

    public static func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        try decoder().decode(type, from: data)
    }

    private static func format(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    private static func parseDate(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: value) {
            return date
        }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: value)
    }
}

private func rejectUnknownKeys<K: CodingKey>(
    in container: KeyedDecodingContainer<K>,
    allowed: Set<String>
) throws {
    let unknown = Set(container.allKeys.map(\.stringValue)).subtracting(allowed)
    if !unknown.isEmpty {
        throw AdminCodecError.unexpectedKeys(unknown)
    }
}

private func requireOnlyKeys<K: CodingKey>(
    _ container: KeyedDecodingContainer<K>,
    _ allowed: [K]
) throws {
    let allowedNames = Set(allowed.map(\.stringValue))
    let actualNames = Set(container.allKeys.map(\.stringValue))
    let unexpected = actualNames.subtracting(allowedNames)
    let missing = allowedNames.subtracting(actualNames)
    if !unexpected.isEmpty {
        throw AdminCodecError.unexpectedKeys(unexpected)
    }
    if !missing.isEmpty {
        throw AdminCodecError.missingKeys(missing)
    }
}
