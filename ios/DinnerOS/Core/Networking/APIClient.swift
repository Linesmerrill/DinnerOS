import Foundation

/// One call to the DinnerOS API.
nonisolated struct APIRequest: Sendable {
    enum Method: String, Sendable {
        case get = "GET"
        case post = "POST"
        case patch = "PATCH"
        case delete = "DELETE"
    }

    var method: Method
    /// Absolute path under the base URL, for example `/api/v1/me`.
    var path: String
    var body: Data?
    var bearerToken: String?
    /// Sent in order. Names and values are percent-encoded strictly, so `+` and `&` in a
    /// value arrive as written.
    var queryItems: [URLQueryItem] = []

    static func get(_ path: String) -> APIRequest {
        APIRequest(method: .get, path: path, body: nil, bearerToken: nil)
    }

    static func post(_ path: String, body: some Encodable) throws -> APIRequest {
        APIRequest(method: .post, path: path, body: try JSONCoding.makeEncoder().encode(body), bearerToken: nil)
    }

    static func patch(_ path: String, body: some Encodable) throws -> APIRequest {
        APIRequest(method: .patch, path: path, body: try JSONCoding.makeEncoder().encode(body), bearerToken: nil)
    }

    static func delete(_ path: String) -> APIRequest {
        APIRequest(method: .delete, path: path, body: nil, bearerToken: nil)
    }

    func authorized(with accessToken: String) -> APIRequest {
        var copy = self
        copy.bearerToken = accessToken
        return copy
    }
}

/// Thin async/await client for the DinnerOS API.
///
/// Encoding, the network round trip, and decoding run off the main actor. The client
/// knows nothing about sessions: `AuthSession` attaches tokens and handles refresh.
/// Nothing here logs request bodies or headers, because they carry tokens.
nonisolated struct APIClient: Sendable {
    let baseURL: URL
    let transport: any HTTPTransport

    init(baseURL: URL, transport: any HTTPTransport = URLSessionTransport()) {
        self.baseURL = baseURL
        self.transport = transport
    }

    /// Sends `request` and decodes a JSON response body.
    @concurrent
    func send<Response: Decodable & Sendable>(
        _ request: APIRequest, as type: Response.Type = Response.self
    ) async throws -> Response {
        let data = try await perform(request)
        do {
            return try JSONCoding.makeDecoder().decode(Response.self, from: data)
        } catch {
            throw APIError.decoding(type: String(describing: Response.self))
        }
    }

    /// Sends `request` and ignores any response body (for example, `204 No Content`).
    @concurrent
    func sendIgnoringBody(_ request: APIRequest) async throws {
        _ = try await perform(request)
    }

    func makeURLRequest(for request: APIRequest, requestID: String) -> URLRequest {
        let relativePath = request.path.hasPrefix("/") ? String(request.path.dropFirst()) : request.path
        var url = baseURL.appending(path: relativePath)
        if !request.queryItems.isEmpty, var components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
            components.percentEncodedQuery = request.queryItems
                .map { Self.encodeQueryComponent($0.name) + "=" + Self.encodeQueryComponent($0.value ?? "") }
                .joined(separator: "&")
            url = components.url ?? url
        }
        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = request.method.rawValue
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")
        urlRequest.setValue(requestID, forHTTPHeaderField: "X-Request-ID")
        if let body = request.body {
            urlRequest.httpBody = body
            urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let token = request.bearerToken {
            urlRequest.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return urlRequest
    }

    /// RFC 3986 unreserved characters only. `URLQueryItem`'s default encoding leaves `+`
    /// as is, which Go's query parser reads as a space.
    private static let queryAllowed = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    static func encodeQueryComponent(_ string: String) -> String {
        string.addingPercentEncoding(withAllowedCharacters: queryAllowed) ?? ""
    }

    private func perform(_ request: APIRequest) async throws -> Data {
        let requestID = UUID().uuidString
        let urlRequest = makeURLRequest(for: request, requestID: requestID)

        let data: Data
        let response: HTTPURLResponse
        do {
            (data, response) = try await transport.send(urlRequest)
        } catch let error as APIError {
            throw error
        } catch let error as URLError {
            if error.code == .cancelled { throw CancellationError() }
            throw APIError.transport(error.code)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw APIError.transport(.unknown)
        }

        guard (200..<300).contains(response.statusCode) else {
            throw APIError(
                status: response.statusCode,
                body: data,
                fallbackRequestID: response.value(forHTTPHeaderField: "X-Request-ID") ?? requestID)
        }
        return data
    }
}
