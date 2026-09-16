#if DEBUG

    import Foundation

    /// A test identity for `POST /auth/dev`, derived from the subject alone.
    ///
    /// The API creates a user the first time it sees a subject, so a second subject is a
    /// second person: it is the only way to reach a two-person household from the app.
    /// Debug builds only, and the screen offering it is additionally gated on
    /// `AppConfiguration.Environment.development`.
    struct DeveloperIdentity: Equatable, Sendable {
        /// The provider subject. Trimmed, lowercased, never empty.
        let subject: String
        /// `<subject>@example.com`, with anything an address can't hold replaced. The API
        /// rejects an invalid address, which would fail a sign-in that would otherwise work.
        let email: String
        /// The subject as words, so two test users aren't both "Simulator Developer".
        let displayName: String

        /// The subject the screen starts on, and the one every earlier build used.
        static let defaultSubject = "dev-simulator"

        /// Offered in the picker. Free text covers everything else.
        static let suggestedSubjects = [defaultSubject, "rachel-sim", "charlie-sim"]

        /// Characters an email local part may hold here. `validEmail` on the API is
        /// stricter than RFC 5322, so this stays to the boring subset.
        private static let emailSafe = CharacterSet.lowercaseLetters
            .union(.decimalDigits)
            .union(CharacterSet(charactersIn: "-_."))

        init(subject rawSubject: String) {
            let normalized = rawSubject.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            let subject = normalized.isEmpty ? Self.defaultSubject : normalized
            self.subject = subject
            email = "\(Self.localPart(for: subject))@example.com"
            displayName = Self.displayName(for: subject)
        }

        /// The subject reduced to address-safe characters, falling back to the default
        /// subject when nothing usable is left (a subject of punctuation alone).
        private static func localPart(for subject: String) -> String {
            let mapped = String(
                subject.unicodeScalars.map { emailSafe.contains($0) ? Character($0) : "-" })
            let trimmed = mapped.trimmingCharacters(in: CharacterSet(charactersIn: "-."))
            return trimmed.isEmpty ? defaultSubject : trimmed
        }

        /// `rachel-sim` → "Rachel Sim". Separators are anything that isn't alphanumeric.
        private static func displayName(for subject: String) -> String {
            let words = subject.split(whereSeparator: { !$0.isLetter && !$0.isNumber }).map { $0.capitalized }
            return words.isEmpty ? defaultSubject : words.joined(separator: " ")
        }
    }

#endif
