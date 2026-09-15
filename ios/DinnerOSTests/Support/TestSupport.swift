import Foundation
import Synchronization

@testable import DinnerOS

/// An `HTTPTransport` that answers requests with a closure and records them.
nonisolated final class StubTransport: HTTPTransport {
    typealias Handler = @Sendable (URLRequest) async throws -> (status: Int, body: Data)

    private let handler: Handler
    private let recorded = Mutex<[URLRequest]>([])

    init(_ handler: @escaping Handler) {
        self.handler = handler
    }

    var requests: [URLRequest] {
        recorded.withLock { $0 }
    }

    func requests(to path: String) -> [URLRequest] {
        requests.filter { $0.url?.path() == path }
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        recorded.withLock { $0.append(request) }
        let (status, body) = try await handler(request)
        guard
            let url = request.url,
            let response = HTTPURLResponse(
                url: url, statusCode: status, httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/json"])
        else { throw URLError(.badServerResponse) }
        return (body, response)
    }
}

/// A thread-safe call counter for stub handlers.
nonisolated final class Counter: Sendable {
    private let storage = Mutex(0)

    var value: Int { storage.withLock { $0 } }

    func increment() {
        storage.withLock { $0 += 1 }
    }
}

nonisolated enum Fixtures {
    static let baseURLString = "https://api.example.test"

    static let user = UserSummary(
        id: "66e5a1f2c3b4a5d6e7f80912",
        displayName: "Ada Lovelace",
        primaryEmail: "ada@example.com",
        createdAt: Date(timeIntervalSince1970: 1_757_000_000))

    static func tokens(
        access: String = "access-1",
        refresh: String = "refresh-1",
        accessExpiresIn: TimeInterval = 900,
        refreshExpiresIn: TimeInterval = 60 * 60 * 24 * 60
    ) -> AuthTokens {
        AuthTokens(
            accessToken: access,
            accessTokenExpiresAt: Date().addingTimeInterval(accessExpiresIn).rounded(),
            refreshToken: refresh,
            refreshTokenExpiresAt: Date().addingTimeInterval(refreshExpiresIn).rounded())
    }

    static func iso(_ date: Date) -> String {
        date.formatted(.iso8601)
    }

    static func tokenPairJSON(access: String, refresh: String) -> Data {
        Data(
            """
            {"accessToken":"\(access)","accessTokenExpiresAt":"\(iso(Date().addingTimeInterval(900)))",
             "refreshToken":"\(refresh)","refreshTokenExpiresAt":"\(iso(Date().addingTimeInterval(5_184_000)))"}
            """.utf8)
    }

    static func sessionJSON(access: String, refresh: String, displayName: String = "Ada Lovelace") -> Data {
        Data(
            """
            {"accessToken":"\(access)","accessTokenExpiresAt":"\(iso(Date().addingTimeInterval(900)))",
             "refreshToken":"\(refresh)","refreshTokenExpiresAt":"\(iso(Date().addingTimeInterval(5_184_000)))",
             "user":{"id":"\(user.id)","displayName":"\(displayName)","primaryEmail":"ada@example.com",
                     "createdAt":"2025-09-08T15:33:20.123456789Z"},
             "isNewUser":true}
            """.utf8)
    }

    static let meJSON = Data(
        """
        {"user":{"id":"66e5a1f2c3b4a5d6e7f80912","displayName":"Ada Lovelace","primaryEmail":"ada@example.com",
                 "createdAt":"2025-09-08T15:33:20Z"},
         "identities":[{"provider":"apple","email":"ada@example.com"}]}
        """.utf8)

    static func errorJSON(code: String, message: String = "error", requestID: String? = "req-12345678") -> Data {
        let requestIDField = requestID.map { #","requestId":"\#($0)""# } ?? ""
        return Data(#"{"error":{"code":"\#(code)","message":"\#(message)"\#(requestIDField)}}"#.utf8)
    }
}

nonisolated extension Date {
    /// Whole seconds, so dates survive a JSON round trip exactly.
    func rounded() -> Date {
        Date(timeIntervalSince1970: timeIntervalSince1970.rounded())
    }
}

nonisolated extension URLRequest {
    var bearerToken: String? {
        guard let header = value(forHTTPHeaderField: "Authorization"), header.hasPrefix("Bearer ") else {
            return nil
        }
        return String(header.dropFirst("Bearer ".count))
    }

    /// The JSON body as a string dictionary (all request bodies here are flat strings).
    var jsonBody: [String: String]? {
        guard let httpBody else { return nil }
        return try? JSONDecoder().decode([String: String].self, from: httpBody)
    }

    /// A form-encoded body as a dictionary.
    var formBody: [String: String] {
        guard let httpBody, let string = String(data: httpBody, encoding: .utf8) else { return [:] }
        var fields: [String: String] = [:]
        for pair in string.split(separator: "&") {
            let parts = pair.split(separator: "=", maxSplits: 1).map(String.init)
            guard parts.count == 2 else { continue }
            fields[parts[0].removingPercentEncoding ?? parts[0]] = parts[1].removingPercentEncoding ?? parts[1]
        }
        return fields
    }
}
