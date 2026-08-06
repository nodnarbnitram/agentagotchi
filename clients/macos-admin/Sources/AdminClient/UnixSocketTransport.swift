import Foundation
import Darwin

/// Newline-delimited JSON transport for the Edge's owner-only Unix socket.
/// The Edge handles one admin request per connection, so path-based clients
/// create one short-lived descriptor for each request. A descriptor initializer
/// is provided for deterministic socketpair-based tests.
public final class UnixSocketTransport: AdminTransport, @unchecked Sendable {
    public static let defaultSocketPathTemplate = "~/Library/Application Support/Agentagotchi/edge.sock"
    public static let defaultSocketPath = UnixSocketTransport.expandHome(defaultSocketPathTemplate)

    public let socketPath: String?

    private let injectedDescriptor: Int32?
    private let closeInjectedDescriptor: Bool
    private let timeout: TimeInterval
    private let lock = NSLock()

    /// Creates a transport that connects to `socketPath` for every request.
    /// A leading `~/` is expanded using the current user's home directory.
    public init(socketPath: String = UnixSocketTransport.defaultSocketPath, timeout: TimeInterval = 3) {
        self.socketPath = Self.expandHome(socketPath)
        self.injectedDescriptor = nil
        self.closeInjectedDescriptor = false
        self.timeout = timeout
    }

    /// Creates a transport around an already-connected descriptor. This is
    /// intentionally public so tests can pass one side of `socketpair()`;
    /// production callers should use the path initializer.
    public init(fileDescriptor: Int32, closeOnDeinit: Bool = true, timeout: TimeInterval = 3) {
        self.socketPath = nil
        self.injectedDescriptor = fileDescriptor
        self.closeInjectedDescriptor = closeOnDeinit
        self.timeout = timeout
    }

    deinit {
        if let descriptor = injectedDescriptor, closeInjectedDescriptor {
            _ = Darwin.close(descriptor)
        }
    }

    public func send(_ request: AdminRequest) throws -> AdminResponse {
        let frame: Data
        do {
            frame = try AdminCodec.encode(request)
        } catch {
            throw AdminCodecError.invalidFrame("could not encode administration request")
        }
        guard frame.count + 1 <= AdminCodec.maxFrameBytes else {
            throw AdminTransportError.frameTooLarge
        }

        lock.lock()
        defer { lock.unlock() }

        let descriptor: Int32
        let ownsDescriptor: Bool
        if let injectedDescriptor {
            descriptor = injectedDescriptor
            ownsDescriptor = false
        } else {
            guard let socketPath else {
                throw AdminTransportError.connectionFailed(0)
            }
            descriptor = try connect(to: socketPath, timeout: timeout)
            ownsDescriptor = true
        }
        defer {
            if ownsDescriptor {
                _ = Darwin.close(descriptor)
            }
        }

        try setTimeout(on: descriptor, timeout: timeout)
        var framed = frame
        framed.append(0x0a)
        try writeAll(framed, to: descriptor)
        let replyFrame = try readLine(from: descriptor)
        do {
            return try AdminCodec.decode(AdminResponse.self, from: replyFrame)
        } catch let error as AdminTransportError {
            throw error
        } catch {
            throw AdminCodecError.invalidFrame("could not decode administration response")
        }
    }

    private static func expandHome(_ path: String) -> String {
        guard path == "~" || path.hasPrefix("~/") else { return path }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return path == "~" ? home : home + String(path.dropFirst())
    }
}

private func connect(to path: String, timeout: TimeInterval) throws -> Int32 {
    let descriptor = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
    guard descriptor >= 0 else {
        throw AdminTransportError.socketCreationFailed(Darwin.errno)
    }
    do {
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)
        let pathData = Data(path.utf8) + Data([0])
        let pathCapacity = MemoryLayout.size(ofValue: address.sun_path)
        guard pathData.count <= pathCapacity else {
            throw AdminCodecError.invalidFrame("administration socket path is too long")
        }
        withUnsafeMutableBytes(of: &address.sun_path) { buffer in
            buffer.initializeMemory(as: UInt8.self, repeating: 0)
            pathData.copyBytes(to: buffer)
        }

        let result = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(descriptor, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard result == 0 else {
            throw AdminTransportError.connectionFailed(Darwin.errno)
        }
        try setTimeout(on: descriptor, timeout: timeout)
        return descriptor
    } catch {
        _ = Darwin.close(descriptor)
        throw error
    }
}

private func setTimeout(on descriptor: Int32, timeout: TimeInterval) throws {
    let seconds = max(0, timeout)
    var value = timeval(
        tv_sec: Int(seconds),
        tv_usec: Int32((seconds - floor(seconds)) * 1_000_000)
    )
    let size = socklen_t(MemoryLayout<timeval>.size)
    let receive = setsockopt(descriptor, SOL_SOCKET, SO_RCVTIMEO, &value, size)
    let send = setsockopt(descriptor, SOL_SOCKET, SO_SNDTIMEO, &value, size)
    guard receive == 0, send == 0 else {
        throw AdminTransportError.readFailed(Darwin.errno)
    }
}

private func writeAll(_ data: Data, to descriptor: Int32) throws {
    try data.withUnsafeBytes { rawBuffer in
        guard let baseAddress = rawBuffer.baseAddress else { return }
        var offset = 0
        while offset < rawBuffer.count {
            let count = Darwin.write(descriptor, baseAddress.advanced(by: offset), rawBuffer.count - offset)
            if count < 0 {
                if Darwin.errno == EINTR { continue }
                throw AdminTransportError.writeFailed(Darwin.errno)
            }
            if count == 0 {
                throw AdminTransportError.connectionClosed
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
            throw AdminTransportError.readFailed(Darwin.errno)
        }
        if count == 0 {
            throw AdminTransportError.connectionClosed
        }
        if byte == 0x0a {
            guard !result.isEmpty else { throw AdminTransportError.emptyFrame }
            return result
        }
        result.append(byte)
        if result.count + 1 > AdminCodec.maxFrameBytes {
            throw AdminTransportError.frameTooLarge
        }
    }
}
