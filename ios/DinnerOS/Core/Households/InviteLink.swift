import Foundation

/// An invitation link such as `dinneros://invite?token=...`.
///
/// The API builds links from `APP_INVITE_URL_BASE` (default `dinneros://invite?token=`),
/// and the scheme is registered from `APP_URL_SCHEME` in `ios/Config/Shared.xcconfig`.
/// The two must match. Tokens are secrets: never log a link or its token.
nonisolated enum InviteLink: Equatable, Sendable {
    static let defaultScheme = "dinneros"
    static let host = "invite"

    case token(String)
    /// The link is an invitation link, but its token is missing or empty.
    case missingToken

    /// Returns `nil` when `url` isn't an invitation link for `scheme`.
    init?(url: URL, scheme: String = InviteLink.defaultScheme) {
        guard
            url.scheme?.lowercased() == scheme.lowercased(),
            let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
            components.host?.lowercased() == Self.host,
            components.path.isEmpty || components.path == "/"
        else { return nil }

        let token =
            components.queryItems?
            .first { $0.name == "token" }?
            .value?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        self = token.isEmpty ? .missingToken : .token(token)
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
