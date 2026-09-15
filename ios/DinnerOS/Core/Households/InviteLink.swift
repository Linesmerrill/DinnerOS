import Foundation

/// An invitation link. Tokens are secrets: never log a link or its token.
///
/// Invitation emails link to `https://api.tlps.dev/invite#token=...` (the API's
/// `APP_INVITE_URL_BASE`). With the app installed, iOS opens that universal link in the
/// app; the domain is `APP_LINK_DOMAIN` in `ios/Config/Shared.xcconfig`. Without the app,
/// the API's landing page opens instead, and its button uses the custom-scheme form
/// `dinneros://invite?token=...` (`APP_URL_SCHEME`). Emails sent before universal links
/// used the custom scheme directly, so both forms are accepted, with the token in either
/// the fragment or the query.
nonisolated enum InviteLink: Equatable, Sendable {
    static let defaultScheme = "dinneros"
    static let defaultWebHost = "api.tlps.dev"
    /// The host of a custom-scheme link.
    static let host = "invite"
    /// The path of a universal link.
    static let webPath = "/invite"

    case token(String)
    /// The link is an invitation link, but its token is missing or empty.
    case missingToken

    /// Returns `nil` when `url` is neither `<scheme>://invite` nor `https://<webHost>/invite`.
    init?(url: URL, scheme: String = InviteLink.defaultScheme, webHost: String = InviteLink.defaultWebHost) {
        guard
            let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
            let urlScheme = components.scheme?.lowercased(),
            let host = components.host?.lowercased()
        else { return nil }

        switch urlScheme {
        case scheme.lowercased():
            guard host == Self.host, components.path.isEmpty || components.path == "/" else { return nil }
        case "https":
            guard
                host == webHost.lowercased(),
                components.port == nil,
                components.path == Self.webPath || components.path == Self.webPath + "/"
            else { return nil }
        default:
            return nil
        }

        let token = Self.token(inFragment: components.percentEncodedFragment) ?? Self.token(in: components.queryItems)
        self = token.map { .token($0) } ?? .missingToken
    }

    /// Reads `token` from a fragment written like a query string (`#token=...`).
    private static func token(inFragment fragment: String?) -> String? {
        guard let fragment else { return nil }
        let items = fragment.split(separator: "&").map { pair in
            let parts = pair.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            let value = parts.count == 2 ? String(parts[1]) : ""
            return URLQueryItem(name: String(parts[0]), value: value.removingPercentEncoding ?? value)
        }
        return token(in: items)
    }

    private static func token(in items: [URLQueryItem]?) -> String? {
        let value = items?.first { $0.name == "token" }?.value?.trimmingCharacters(in: .whitespacesAndNewlines)
        return value?.isEmpty == false ? value : nil
    }
}

/// Typed invite codes.
nonisolated enum InviteCode {
    /// Hyphen-minus plus the typographic dashes iOS smart punctuation may substitute.
    private static let dashes = CharacterSet(charactersIn: "-\u{2010}\u{2011}\u{2012}\u{2013}\u{2014}\u{2015}\u{2212}")

    /// Uppercases and removes whitespace and dashes (including typographic dashes that
    /// smart punctuation may insert). Everything else, including validation and reading
    /// O, I, and L as digits, is left to the server.
    static func normalize(_ input: String) -> String {
        let removed = CharacterSet.whitespacesAndNewlines.union(dashes)
        let scalars = input.uppercased().unicodeScalars.filter { !removed.contains($0) }
        return String(String.UnicodeScalarView(scalars))
    }
}
