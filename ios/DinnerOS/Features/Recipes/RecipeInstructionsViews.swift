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
    /// On the cooking screen, times in the step ("8-10 minutes") are tappable timers.
    var showsTimers = false

    var body: some View {
        // A step that is really several ("Heat the oil. \n Stir in the garlic.") reads as
        // separate paragraphs with space between them, not one block.
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(paragraphs.enumerated()), id: \.offset) { _, paragraph in
                composed(paragraph)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(step.spokenText))
    }

    /// The step's segments split at line breaks, blank paragraphs dropped.
    private var paragraphs: [[InstructionSegment]] {
        let segments = step.segments.isEmpty ? [InstructionSegment(kind: .text, text: step.text)] : step.segments
        var out: [[InstructionSegment]] = [[]]
        for segment in segments {
            guard !segment.isIngredient, segment.text.contains("\n") else {
                out[out.count - 1].append(segment)
                continue
            }
            let pieces = segment.text.components(separatedBy: "\n")
            for (i, piece) in pieces.enumerated() {
                if i > 0 { out.append([]) }
                let text = i > 0 ? String(piece.drop { $0 == " " }) : piece
                if !text.isEmpty { out[out.count - 1].append(InstructionSegment(kind: .text, text: text)) }
            }
        }
        return out.filter { paragraph in
            paragraph.contains { !$0.text.trimmingCharacters(in: .whitespaces).isEmpty }
        }
    }

    /// `Text` concatenation keeps one paragraph that wraps and scales with Dynamic Type.
    private func composed(_ segments: [InstructionSegment]) -> Text {
        var text = Text(verbatim: "")
        var soFar = ""
        let names = step.segments.filter(\.isIngredient).map { $0.name ?? $0.text }
        for segment in segments {
            if segment.isIngredient {
                text = text + Self.text(for: segment)
            } else if showsTimers {
                // A time is named for what the sentence is cooking: "…breaking up meat, until
                // browned, 4-6 minutes" is a Beef timer, never the salt added along the way.
                let before = soFar
                text =
                    text
                    + CookTimerText.text(segment.text, step: step.index) { inSegment in
                        CookTimerSubject.pick(sentence: before + inSegment, ingredients: names)
                    }
            } else {
                text = text + Self.text(for: segment)
            }
            soFar += segment.text
        }
        return text
    }

    static func text(for segment: InstructionSegment) -> Text {
        guard segment.isIngredient else { return Text(verbatim: segment.text) }
        // Left out: struck through and quiet, never removed, so the step still reads as written
        // and says the recipe calls for it.
        if segment.leftOut {
            return Text(verbatim: segment.text).strikethrough().foregroundStyle(Color.secondary)
        }
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
                    .opacity(step.leftOut ? 0.6 : 1)
                if step.leftOut {
                    Label("Nothing to do here: you leave out everything in this step.", systemImage: "forward")
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                }
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
            Image(systemName: note.isLeftOut ? "minus.circle" : "arrow.triangle.swap")
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
        .accessibilityLabel(note.isLeftOut ? "Left out. \(note.text)" : "Substitution. \(note.text)")
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
