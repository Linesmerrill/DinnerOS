import SwiftUI

/// The cooking steps as a member reads them: every ingredient the recipe lists is bold with
/// its amount, and anything spicy is bold, red, and flagged with a flame so the heat reads
/// without relying on colour.
///
/// The API decides the words (`RecipeInstructions`); this file decides only the type.

extension Color {
    /// Heat. Paired with a flame and bold weight, never the only signal.
    static let spicyIngredient = Color(.systemRed)
}

/// One step's text, composed from the server's segments.
struct InstructionStepText: View {
    let step: InstructionStep

    var body: some View {
        composed
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityLabel(Text(step.spokenText))
    }

    /// `Text` concatenation keeps one paragraph that wraps and scales with Dynamic Type.
    private var composed: Text {
        guard !step.segments.isEmpty else { return Text(verbatim: step.text) }
        return step.segments.reduce(Text(verbatim: "")) { partial, segment in
            partial + Self.text(for: segment)
        }
    }

    static func text(for segment: InstructionSegment) -> Text {
        guard segment.isIngredient else { return Text(verbatim: segment.text) }
        let named = Text(verbatim: segment.text).fontWeight(.semibold)
        guard segment.spicy else { return named }
        // A flame and the weight carry the meaning; red only reinforces it.
        return Text(Image(systemName: "flame.fill")).foregroundStyle(Color.spicyIngredient)
            + Text(verbatim: "\u{2009}")
            + named.foregroundStyle(Color.spicyIngredient)
    }
}

/// A step: its number, its text, any substitution notes, and its photo.
struct InstructionStepRow: View {
    let step: InstructionStep

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(step.index.formatted())
                .font(.headline)
                .foregroundStyle(.tint)
                .frame(minWidth: 20, alignment: .trailing)
                .accessibilityLabel("Step \(step.index)")
            VStack(alignment: .leading, spacing: 10) {
                InstructionStepText(step: step)
                ForEach(step.notes) { note in
                    InstructionNoteLabel(note: note)
                }
                if let url = step.imageURL {
                    RecipePhoto(url: url, aspectRatio: 16.0 / 9.0, pointWidth: 340, cornerRadius: 12)
                }
            }
        }
    }
}

/// "Instead of 1 tbsp Tex-Mex Paste, use 2 tsp tomato paste and ½ tsp chili powder."
struct InstructionNoteLabel: View {
    let note: InstructionNote

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 6))
        layout {
            Image(systemName: "arrow.triangle.swap")
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text(note.text)
                .fixedSize(horizontal: false, vertical: true)
        }
        .font(.subheadline)
        .foregroundStyle(Color.secondary)
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.quaternary.opacity(0.4), in: .rect(cornerRadius: 10))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Substitution. \(note.text)")
    }
}

/// What the household is cooking with instead of the card's blends and sauces, under the
/// steps, with a way into the setup screen for anything nobody has chosen yet.
struct InstructionSubstitutionsFooter: View {
    let instructions: RecipeInstructions
    let chooseSpecialty: ((InstructionSpecialtyRef) -> Void)?

    var body: some View {
        if !instructions.substitutions.isEmpty || !instructions.unchosenSpecialties.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text("Your Substitutes")
                    .font(.subheadline.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                ForEach(instructions.substitutions) { substitution in
                    Text(substitution.isDefaultChoice ? "\(substitution.text) (your default)" : substitution.text)
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ForEach(instructions.unchosenSpecialties) { specialty in
                    unchosenRow(specialty)
                }
            }
            .padding(.top, 4)
        }
    }

    @ViewBuilder
    private func unchosenRow(_ specialty: InstructionSpecialtyRef) -> some View {
        let text = Text("\(specialty.name): no substitute chosen, so the steps read as the card wrote it.")
            .font(.footnote)
            .foregroundStyle(Color.secondary)
            .fixedSize(horizontal: false, vertical: true)
        if let chooseSpecialty {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                text
                Button("Choose") { chooseSpecialty(specialty) }
                    .font(.footnote.weight(.semibold))
                    .buttonStyle(.plain)
                    .foregroundStyle(.tint)
            }
        } else {
            text
        }
    }
}

#Preview("Step") {
    let segments = [
        InstructionSegment(kind: .text, text: "Whisk "),
        InstructionSegment(
            kind: .ingredient, text: "2 tbsp gochujang", name: "Gochujang",
            amount: InstructionAmount(quantity: "2", quantityValue: 2, unit: "tbsp", text: "2 tbsp"), spicy: true),
        InstructionSegment(kind: .text, text: " into "),
        InstructionSegment(
            kind: .ingredient, text: "1 tbsp Tomato Paste", name: "Tomato Paste", substituted: true,
            specialtyID: "tex-mex-paste", specialtyName: "Tex-Mex Paste"),
        InstructionSegment(kind: .text, text: ", then simmer."),
    ]
    let note = InstructionNote(
        kind: "substitution", specialtyID: "tex-mex-paste",
        text: "Instead of 1 tbsp Tex-Mex Paste, use 2 tsp Tomato Paste and ½ tsp Chili Powder.")
    return ScrollView {
        InstructionStepRow(
            step: InstructionStep(
                index: 1, text: "Whisk 2 tbsp gochujang into 1 tbsp Tomato Paste, then simmer.",
                originalText: "Whisk the gochujang into the Tex-Mex Paste, then simmer.",
                segments: segments, notes: [note])
        )
        .padding()
    }
}
