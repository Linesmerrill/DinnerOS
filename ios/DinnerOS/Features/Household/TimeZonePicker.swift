import SwiftUI

/// A searchable list of IANA time zones.
struct TimeZonePicker: View {
    @Binding var selection: String

    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    private static let identifiers = TimeZone.knownTimeZoneIdentifiers.sorted()

    private var results: [String] {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return Self.identifiers }
        return Self.identifiers.filter {
            $0.replacing("_", with: " ").localizedStandardContains(trimmed)
                || Self.localizedName(for: $0).localizedStandardContains(trimmed)
        }
    }

    var body: some View {
        List(results, id: \.self) { identifier in
            Button {
                selection = identifier
                dismiss()
            } label: {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(Self.cityName(for: identifier))
                            .foregroundStyle(.primary)
                        Text("\(Self.localizedName(for: identifier)) · \(identifier)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if identifier == selection {
                        Image(systemName: "checkmark")
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                    }
                }
                .contentShape(.rect)
            }
            .accessibilityAddTraits(identifier == selection ? .isSelected : [])
        }
        .overlay {
            if results.isEmpty {
                ContentUnavailableView.search(text: query)
            }
        }
        .searchable(text: $query, prompt: "City or time zone")
        .navigationTitle("Time Zone")
    }

    /// "Denver (Mountain Time)" for "America/Denver".
    static func summary(for identifier: String) -> String {
        "\(cityName(for: identifier)) (\(localizedName(for: identifier)))"
    }

    static func cityName(for identifier: String) -> String {
        identifier.split(separator: "/").last.map { String($0).replacing("_", with: " ") } ?? identifier
    }

    static func localizedName(for identifier: String) -> String {
        TimeZone(identifier: identifier)?.localizedName(for: .generic, locale: .current) ?? identifier
    }
}

#Preview {
    @Previewable @State var selection = "America/Denver"
    NavigationStack {
        TimeZonePicker(selection: $selection)
    }
}
