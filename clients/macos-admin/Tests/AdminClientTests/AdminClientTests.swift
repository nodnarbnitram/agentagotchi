import Foundation
import Darwin

#if canImport(XCTest)
import XCTest

@testable import AdminClient

final class AdminClientTests: XCTestCase {
    func testEncodesAdminRequestsWithExactSchemaAndFields() throws {
        let data = try AdminCodec.encode(
            AdminRequest.pairingBegin(role: .edgeIngress, clientName: "local-edge")
        )
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])

        XCTAssertEqual(object["schema"] as? String, "agentagotchi.admin.v1")
        XCTAssertEqual(object["type"] as? String, "pairing_begin")
        XCTAssertEqual(object["role"] as? String, "edge-ingress")
        XCTAssertEqual(object["clientName"] as? String, "local-edge")
        XCTAssertEqual(Set(object.keys), ["schema", "type", "role", "clientName"])

        let status = try AdminCodec.encode(AdminRequest.status)
        let statusObject = try XCTUnwrap(JSONSerialization.jsonObject(with: status) as? [String: Any])
        XCTAssertEqual(Set(statusObject.keys), ["schema", "type"])
        XCTAssertEqual(statusObject["type"] as? String, "status")
    }

    func testEncodesEveryPairingRequestShape() throws {
        let cases: [(AdminRequest, String, String, String?)] = [
            (.pairingPending, "pairing_pending", "", nil),
            (.pairingApprove(codeID: "code-id"), "pairing_approve", "codeId", "code-id"),
            (.pairingDeny(codeID: "code-id"), "pairing_deny", "codeId", "code-id"),
            (.pairingList, "pairing_list", "", nil),
            (.pairingRevoke(credentialID: "credential-id"), "pairing_revoke", "credentialId", "credential-id"),
        ]
        for (request, type, key, value) in cases {
            let data = try AdminCodec.encode(request)
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
            XCTAssertEqual(object["schema"] as? String, AdminSchema.v1)
            XCTAssertEqual(object["type"] as? String, type)
            if key.isEmpty {
                XCTAssertEqual(Set(object.keys), ["schema", "type"])
            } else {
                XCTAssertEqual(object[key] as? String, value)
                XCTAssertEqual(Set(object.keys), ["schema", "type", key])
            }
        }
    }

    func testDecodesStatusAndRedactedCredentialReply() throws {
        let json = """
        {
          "schema":"agentagotchi.admin.v1",
          "type":"status",
          "ok":true,
          "status":{
            "schema":"agentagotchi.admin.v1",
            "type":"status",
            "role":"edge",
            "version":"0.2.0",
            "startedAt":"2025-01-02T03:04:05.123Z",
            "pairedDevices":2,
            "connectedPeers":1,
            "taskPresences":4,
            "aggregateState":"needs_input"
          }
        }
        """.data(using: .utf8)!

        let response = try AdminCodec.decode(AdminResponse.self, from: json)
        XCTAssertTrue(response.ok)
        XCTAssertEqual(response.type, "status")
        XCTAssertEqual(response.status?.role, "edge")
        XCTAssertEqual(response.status?.pairedDevices, 2)
        XCTAssertEqual(response.status?.pairedEdges, 0)
        XCTAssertEqual(response.status?.aggregateState, "needs_input")
        let startedAt = try XCTUnwrap(response.status?.startedAt)
        XCTAssertEqual(startedAt.timeIntervalSince1970, 1735787045.123, accuracy: 0.001)

        let credentialJSON = """
        {
          "schema":"agentagotchi.admin.v1",
          "type":"pairing_list",
          "ok":true,
          "credentials":[{
            "id":"credential-id",
            "role":"feed",
            "clientName":"BOX-3",
            "issuedAt":"2025-01-02T03:04:05Z",
            "revoked":false
          }]
        }
        """.data(using: .utf8)!
        let credentialResponse = try AdminCodec.decode(AdminResponse.self, from: credentialJSON)
        XCTAssertNil(credentialResponse.credentials?.first?.token)
        XCTAssertEqual(credentialResponse.credentials?.first?.role, .feed)
    }

    func testRejectsUnknownFieldsAndWrongSchema() throws {
        let unknown = """
        {"schema":"agentagotchi.admin.v1","type":"status","unexpected":true}
        """.data(using: .utf8)!
        XCTAssertThrowsError(try AdminCodec.decode(AdminRequest.self, from: unknown)) { error in
            guard case AdminCodecError.unexpectedKeys = error else {
                return XCTFail("unexpected error: \(error)")
            }
        }

        let wrongSchema = """
        {"schema":"agentagotchi.other.v1","type":"status"}
        """.data(using: .utf8)!
        XCTAssertThrowsError(try AdminCodec.decode(AdminRequest.self, from: wrongSchema)) { error in
            guard case AdminCodecError.schemaMismatch = error else {
                return XCTFail("unexpected error: \(error)")
            }
        }
    }

    func testRoundTripAgainstFakeAdminServerUsingSocketPair() throws {
        let descriptors = try makeSocketPair()
        let transport = UnixSocketTransport(fileDescriptor: descriptors.client, timeout: 2)
        let serverFinished = expectation(description: "fake admin server finished")
        var receivedRequest: AdminRequest?
        var serverError: Error?

        DispatchQueue.global(qos: .userInitiated).async {
            defer {
                _ = Darwin.close(descriptors.server)
                serverFinished.fulfill()
            }
            do {
                let requestData = try readLine(from: descriptors.server)
                receivedRequest = try AdminCodec.decode(AdminRequest.self, from: requestData)

                let status = AdminStatus(
                    role: "edge",
                    version: "test",
                    startedAt: Date(timeIntervalSince1970: 1_700_000_000),
                    pairedDevices: 1,
                    pairedEdges: 0,
                    connectedPeers: 2,
                    taskPresences: 3,
                    aggregateState: "running"
                )
                let response = AdminResponse(type: "status", ok: true, status: status)
                var reply = try AdminCodec.encode(response)
                reply.append(0x0a)
                try writeAll(reply, to: descriptors.server)
            } catch {
                serverError = error
            }
        }

        let response = try transport.send(.status)
        wait(for: [serverFinished], timeout: 2)

        XCTAssertNil(serverError)
        XCTAssertEqual(receivedRequest, .status)
        XCTAssertEqual(response.status?.version, "test")
        XCTAssertEqual(response.status?.taskPresences, 3)
    }

    private func makeSocketPair() throws -> (client: Int32, server: Int32) {
        var descriptors = [Int32](repeating: 0, count: 2)
        guard Darwin.socketpair(AF_UNIX, SOCK_STREAM, 0, &descriptors) == 0 else {
            throw NSError(domain: NSPOSIXErrorDomain, code: Int(Darwin.errno))
        }
        return (descriptors[0], descriptors[1])
    }
}

private func writeAll(_ data: Data, to descriptor: Int32) throws {
    try data.withUnsafeBytes { buffer in
        guard let base = buffer.baseAddress else { return }
        var offset = 0
        while offset < buffer.count {
            let count = Darwin.write(descriptor, base.advanced(by: offset), buffer.count - offset)
            if count < 0 {
                if Darwin.errno == EINTR { continue }
                throw NSError(domain: NSPOSIXErrorDomain, code: Int(Darwin.errno))
            }
            if count == 0 {
                throw NSError(domain: NSPOSIXErrorDomain, code: Int(ECONNRESET))
            }
            offset += count
        }
    }
}

private func readLine(from descriptor: Int32) throws -> Data {
    var result = Data()
    var byte: UInt8 = 0
    while true {
        let count = Darwin.read(descriptor, &byte, 1)
        if count < 0 {
            if Darwin.errno == EINTR { continue }
            throw NSError(domain: NSPOSIXErrorDomain, code: Int(Darwin.errno))
        }
        if count == 0 {
            throw NSError(domain: NSPOSIXErrorDomain, code: Int(ECONNRESET))
        }
        if byte == 0x0a { return result }
        result.append(byte)
    }
}
#else

// The standalone Swift toolchain shipped with CommandLineTools does not ship
// XCTest. Keep the same codec/socket checks executable there so `swift test`
// still exercises this target; Xcode uses the XCTest suite above.
import AdminClient

private let runCommandLineToolchainChecks: Void = {
    do {
        let encoded = try AdminCodec.encode(
            AdminRequest.pairingBegin(role: .edgeIngress, clientName: "local-edge")
        )
        let object = try JSONSerialization.jsonObject(with: encoded) as! [String: Any]
        precondition(object["schema"] as? String == AdminSchema.v1)
        precondition(object["type"] as? String == "pairing_begin")
        precondition(object["role"] as? String == "edge-ingress")
        precondition(object["clientName"] as? String == "local-edge")

        let reply = """
        {"schema":"agentagotchi.admin.v1","type":"status","ok":true,"status":{"schema":"agentagotchi.admin.v1","type":"status","role":"edge","version":"test","startedAt":"2025-01-02T03:04:05Z","pairedDevices":1,"connectedPeers":0,"taskPresences":0,"aggregateState":"idle"}}
        """.data(using: .utf8)!
        let decoded = try AdminCodec.decode(AdminResponse.self, from: reply)
        precondition(decoded.status?.role == "edge")
        precondition(decoded.status?.pairedEdges == 0)

        var descriptors = [Int32](repeating: 0, count: 2)
        precondition(Darwin.socketpair(AF_UNIX, SOCK_STREAM, 0, &descriptors) == 0)
        let transport = UnixSocketTransport(fileDescriptor: descriptors[0], timeout: 2)
        DispatchQueue.global().async {
            defer { _ = Darwin.close(descriptors[1]) }
            do {
                _ = try readFallbackLine(from: descriptors[1])
                var data = try AdminCodec.encode(
                    AdminResponse(type: "status", ok: true, status: AdminStatus(
                        role: "edge", version: "test", startedAt: Date(), pairedDevices: 0,
                        connectedPeers: 0, taskPresences: 0, aggregateState: "idle"
                    ))
                )
                data.append(0x0a)
                try writeFallback(data, to: descriptors[1])
            } catch {
                preconditionFailure("fake admin server failed: \(error)")
            }
        }
        let roundTrip = try transport.send(.status)
        precondition(roundTrip.status?.role == "edge")
    } catch {
        preconditionFailure("AdminClient checks failed: \(error)")
    }
}()

private func writeFallback(_ data: Data, to descriptor: Int32) throws {
    try data.withUnsafeBytes { buffer in
        guard let base = buffer.baseAddress else { return }
        var offset = 0
        while offset < buffer.count {
            let count = Darwin.write(descriptor, base.advanced(by: offset), buffer.count - offset)
            if count < 0 {
                if Darwin.errno == EINTR { continue }
                throw NSError(domain: NSPOSIXErrorDomain, code: Int(Darwin.errno))
            }
            offset += count
        }
    }
}

private func readFallbackLine(from descriptor: Int32) throws -> Data {
    var result = Data()
    var byte: UInt8 = 0
    while true {
        let count = Darwin.read(descriptor, &byte, 1)
        if count < 0 {
            if Darwin.errno == EINTR { continue }
            throw NSError(domain: NSPOSIXErrorDomain, code: Int(Darwin.errno))
        }
        if count == 0 { throw NSError(domain: NSPOSIXErrorDomain, code: Int(ECONNRESET)) }
        if byte == 0x0a { return result }
        result.append(byte)
    }
}
#endif
