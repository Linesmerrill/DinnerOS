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
        // A step that is really several ("Heat the oil. • Stir in the garlic.") reads as separate
        // paragraphs, and a sentence that adds several things at once as a list under its verb.
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(runs.enumerated()), id: \.offset) { _, run in
                switch run.block {
                case .prose(let segments):
                    composed(segments, timersBefore: run.timersBefore)
                        .fixedSize(horizontal: false, vertical: true)
                case .list(let lead, let items):
                    StepIngredientGroup(
                        lead: composed(lead, timersBefore: run.timersBefore),
                        items: items.map(Self.itemText))
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(step.spokenText))
    }

    /// The step's blocks in reading order, each with how many times came before it, so each
    /// block's times line up with the server's timers for the step.
    private var runs: [(block: StepBlock, timersBefore: Int)] {
        let segments = step.segments.isEmpty ? [InstructionSegment(kind: .text, text: step.text)] : step.segments
        var out: [(block: StepBlock, timersBefore: Int)] = []
        var timers = 0
        for block in StepBlocks.paragraphs(segments).flatMap(StepBlocks.blocks) {
            out.append((block, timers))
            switch block {
            case .prose(let segments): timers += Self.timerCount(in: segments)
            case .list(let lead, let items): timers += Self.timerCount(in: lead + items.flatMap { $0 })
            }
        }
        return out
    }

    /// How many times the segments' text names.
    private static func timerCount(in segments: [InstructionSegment]) -> Int {
        segments.filter { !$0.isIngredient }.reduce(0) { $0 + CookDurations.find(in: $1.text).count }
    }

    /// One thing a list adds: its amount quiet, its name bold ("½ cup" rice), with any words
    /// before it ("a pinch of") as written.
    static func itemText(_ segments: [InstructionSegment]) -> Text {
        segments.reduce(Text(verbatim: "")) { text, segment in
            guard segment.isIngredient, !segment.leftOut, !segment.spicy, let amount = segment.amount?.text,
                segment.text.hasPrefix(amount + " ")
            else { return text + Self.text(for: segment) }
            return text
                + Text(verbatim: amount + "\u{00A0}").foregroundStyle(Color.secondary)
                + Text(verbatim: String(segment.text.dropFirst(amount.count + 1))).fontWeight(.semibold)
        }
    }

    /// `Text` concatenation keeps one paragraph that wraps and scales with Dynamic Type.
    private func composed(_ segments: [InstructionSegment], timersBefore: Int = 0) -> Text {
        var text = Text(verbatim: "")
        var soFar = ""
        var timerIndex = timersBefore
        let names = step.segments.filter(\.isIngredient).map { $0.name ?? $0.text }
        for segment in segments {
            if segment.isIngredient {
                text = text + Self.text(for: segment)
            } else if showsTimers {
                // A time is named for what the sentence is cooking: "…breaking up meat, until
                // browned, 4-6 minutes" is a Beef timer, never the salt added along the way.
                let before = soFar
                // The server names the timer and says where to start it; the app's own reading
                // is the fallback for an older server.
                text =
                    text
                    + CookTimerText.text(
                        segment.text, step: step.index,
                        subject: { inSegment in
                            CookTimerSubject.pick(sentence: before + inSegment, ingredients: names)
                        },
                        server: { _, written in
                            defer { timerIndex += 1 }
                            guard step.timers.indices.contains(timerIndex), step.timers[timerIndex].text == written
                            else { return nil }
                            return step.timers[timerIndex]
                        })
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

/// "Combine:" and what goes in, indented under a thin rule like the ingredient list's tree lines:
/// two columns on a phone, three with room to spare, one at the largest text sizes, so the list
/// stays short instead of running across the page.
struct StepIngredientGroup: View {
    let lead: Text
    let items: [Text]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var columns: Int {
        if dynamicTypeSize.isAccessibilitySize { return 1 }
        return sizeClass == .regular ? 3 : 2
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            lead
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            HStack(alignment: .top, spacing: 12) {
                Capsule()
                    .fill(Color.accentColor.opacity(0.5))
                    .frame(width: 2)
                Grid(alignment: .topLeading, horizontalSpacing: 14, verticalSpacing: 6) {
                    ForEach(Array(stride(from: 0, to: items.count, by: columns)), id: \.self) { start in
                        GridRow {
                            ForEach(start..<min(start + columns, items.count), id: \.self) { i in
                                items[i]
                                    .fixedSize(horizontal: false, vertical: true)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                    }
                }
            }
            .padding(.leading, 4)
        }
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

/// Under the steps, as on the meal-kit card: how hot to cook the meat and seafood in it.
struct SafeTemperaturesFooter: View {
    let instructions: RecipeInstructions

    var body: some View {
        if !instructions.safeTemperatures.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Label("Cook To", systemImage: "thermometer.medium")
                    .font(.subheadline.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                ForEach(instructions.safeTemperatures) { temperature in
                    Text(temperature.text)
                        .font(.subheadline)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if !instructions.safeTemperaturesSource.isEmpty {
                    Text(instructions.safeTemperaturesSource)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(.quaternary.opacity(0.4), in: .rect(cornerRadius: 12))
            .accessibilityElement(children: .combine)
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
