import Foundation

/// A step laid out for the stove: prose, and the moments it adds several things at once pulled out
/// as a short list under their verb.
///
/// "In a small pot, combine ½ cup rice, ¾ cup water, and a pinch of salt." reads as "In a small
/// pot, combine:" over ½ cup rice / ¾ cup water / a pinch of salt, so the cook sees everything that
/// goes in without reading the sentence twice. The rest of the sentence after the list joins the
/// lead ("Stir into pan with chicken mixture:"). Nothing is reworded beyond dropping the commas and
/// "and" that joined the names, and "•" sub-steps start their own paragraph.
nonisolated enum StepBlock: Equatable {
    case prose([InstructionSegment])
    case list(lead: [InstructionSegment], items: [[InstructionSegment]])
}

nonisolated enum StepBlocks {
    /// A sentence becomes a list once it adds this many things in a row.
    static let minimumItems = 3

    /// Splits a step's segments into paragraphs: at line breaks and at "•".
    static func paragraphs(_ segments: [InstructionSegment]) -> [[InstructionSegment]] {
        var out: [[InstructionSegment]] = [[]]
        for segment in segments {
            guard !segment.isIngredient, segment.text.contains(where: { $0 == "\n" || $0 == "•" }) else {
                out[out.count - 1].append(segment)
                continue
            }
            let pieces = segment.text.split(omittingEmptySubsequences: false) { $0 == "\n" || $0 == "•" }
            for (i, piece) in pieces.enumerated() {
                if i > 0 { out.append([]) }
                var text = String(piece)
                if i > 0 { text = String(text.drop { $0 == " " }) }
                if i < pieces.count - 1 { text = String(text.reversed().drop { $0 == " " }.reversed()) }
                if !text.isEmpty { out[out.count - 1].append(InstructionSegment(kind: .text, text: text)) }
            }
        }
        return out.filter { paragraph in
            paragraph.contains { !$0.text.trimmingCharacters(in: .whitespaces).isEmpty }
        }
    }

    /// One paragraph's blocks: prose, with each sentence that adds several things in a row as a list.
    static func blocks(_ paragraph: [InstructionSegment]) -> [StepBlock] {
        var out: [StepBlock] = []
        var prose: [InstructionSegment] = []
        for sentence in sentences(paragraph) {
            if let list = list(sentence) {
                if !prose.isEmpty { out.append(.prose(prose)) }
                prose = []
                out.append(list)
            } else {
                prose += sentence
            }
        }
        if !prose.isEmpty { out.append(.prose(prose)) }
        return out
    }

    /// The paragraph cut after each ".", "!", "?", or ";" that ends a sentence.
    static func sentences(_ paragraph: [InstructionSegment]) -> [[InstructionSegment]] {
        var out: [[InstructionSegment]] = [[]]
        for segment in paragraph {
            guard !segment.isIngredient else {
                out[out.count - 1].append(segment)
                continue
            }
            var rest = Substring(segment.text)
            while let end = sentenceEnd(in: rest) {
                out[out.count - 1].append(InstructionSegment(kind: .text, text: String(rest[..<end])))
                out.append([])
                rest = rest[end...]
            }
            if !rest.isEmpty { out[out.count - 1].append(InstructionSegment(kind: .text, text: String(rest))) }
        }
        return out.filter { !$0.isEmpty }
    }

    /// Just past a sentence's end and the spaces after it: ". " or a final ".".
    private static func sentenceEnd(in text: Substring) -> Substring.Index? {
        var i = text.startIndex
        while i < text.endIndex {
            let next = text.index(after: i)
            if ".!?;".contains(text[i]), next == text.endIndex || text[next] == " " {
                var end = next
                while end < text.endIndex, text[end] == " " { end = text.index(after: end) }
                // A final "." with nothing after isn't a cut; it stays with its sentence.
                return end == text.endIndex ? nil : end
            }
            i = next
        }
        return nil
    }

    /// What joins two names in a list: a comma or "and", then at most a few words that belong to the
    /// next name ("a pinch of", "juice from", "another large drizzle of").
    private static let joinRe = try? NSRegularExpression(
        pattern: #"^\s*(?:,\s*(?:(?:and|or)\s+)?|(?:and|or)\s+)((?:[\p{L}’'-]+\s+){0,4})$"#)

    /// Words that start a new part of the sentence rather than describe the next name: "and into the
    /// pan with chicken" isn't a list.
    private static let clauseWords: Set<String> = [
        "into", "with", "to", "in", "on", "onto", "over", "then", "until", "for", "the", "your",
    ]

    private static func join(_ text: String) -> String? {
        guard let joinRe else { return nil }
        let ns = text as NSString
        guard let m = joinRe.firstMatch(in: text, range: NSRange(location: 0, length: ns.length)) else { return nil }
        let words = ns.substring(with: m.range(at: 1))
        guard !words.lowercased().split(separator: " ").contains(where: { clauseWords.contains(String($0)) })
        else { return nil }
        return words
    }

    /// The sentence as a list, when it adds at least `minimumItems` things in a row after a lead.
    private static func list(_ sentence: [InstructionSegment]) -> StepBlock? {
        let names = sentence.indices.filter { sentence[$0].isIngredient }
        guard names.count >= minimumItems else { return nil }
        // The longest run of names joined only by commas, "and", and a few words.
        var best: Range<Int>? = nil
        var start = 0
        for k in 1...names.count {
            let joined =
                k < names.count && names[k] - names[k - 1] <= 2
                && join(sentence[(names[k - 1] + 1)..<names[k]].map(\.text).joined()) != nil
            if !joined {
                if k - start >= minimumItems, best.map({ k - start > $0.count }) ?? true { best = start..<k }
                start = k
            }
        }
        guard let run = best else { return nil }
        let first = names[run.lowerBound], last = names[run.upperBound - 1]
        var lead = Array(sentence[..<first])
        var items: [[InstructionSegment]] = [firstItemWords(&lead) + [sentence[first]]]
        guard lead.contains(where: { !$0.isIngredient && !$0.text.trimmingCharacters(in: .whitespaces).isEmpty })
        else { return nil }
        for k in (run.lowerBound + 1)..<run.upperBound {
            let gap = sentence[(names[k - 1] + 1)..<names[k]].map(\.text).joined()
            let words = join(gap) ?? ""
            items.append(
                (words.isEmpty ? [] : [InstructionSegment(kind: .text, text: words)]) + [sentence[names[k]]])
        }
        return .list(lead: leadLine(lead, tail: Array(sentence[(last + 1)...])), items: items)
    }

    /// Words at the end of the lead that belong to the first name ("remaining", "¼ of the", "a pinch
    /// of"), taken off the lead: "Season with remaining" reads "Season with:" over "remaining Paprika".
    private static let firstItemRe = try? NSRegularExpression(
        pattern:
            #"(?i)(?:^|(?<=\s))((?:the\s+)?remaining\s+|half(?:\s+of)?(?:\s+the)?\s+|[\d½¼¾⅓⅔⅛/]+\s+of(?:\s+the)?\s+|a\s+(?:(?:big|large|small)\s+)?(?:pinch|drizzle|splash|squeeze)\s+of\s+|(?:juice|zest)\s+from\s+|the\s+)$"#
    )

    private static func firstItemWords(_ lead: inout [InstructionSegment]) -> [InstructionSegment] {
        guard let i = lead.indices.last, !lead[i].isIngredient, let firstItemRe else { return [] }
        let text = lead[i].text
        let ns = text as NSString
        guard let m = firstItemRe.firstMatch(in: text, range: NSRange(location: 0, length: ns.length)) else {
            return []
        }
        lead[i] = InstructionSegment(kind: .text, text: ns.substring(to: m.range(at: 1).location))
        return [InstructionSegment(kind: .text, text: ns.substring(from: m.range(at: 1).location))]
    }

    /// The lead and the rest of the sentence after the list, ending in a colon: "Stir" and " into
    /// pan with chicken mixture." make "Stir into pan with chicken mixture:".
    private static func leadLine(_ lead: [InstructionSegment], tail: [InstructionSegment]) -> [InstructionSegment] {
        var out = lead
        if let i = out.indices.last, !out[i].isIngredient {
            out[i] = InstructionSegment(kind: .text, text: trimmedEnd(out[i].text))
        }
        var tail = tail
        if let i = tail.indices.last, !tail[i].isIngredient {
            let text = trimmedEnd(tail[i].text)
            tail[i] = InstructionSegment(
                kind: .text, text: ".!?;".contains(text.last ?? " ") ? String(text.dropLast()) : text)
        }
        let tailText = tail.map(\.text).joined().trimmingCharacters(in: .whitespaces)
        if !tailText.isEmpty {
            if let i = tail.indices.first, !tail[i].isIngredient, !tail[i].text.hasPrefix(" ") {
                tail[i] = InstructionSegment(kind: .text, text: " " + tail[i].text)
            }
            out += tail
        }
        return out + [InstructionSegment(kind: .text, text: ":")]
    }

    private static func trimmedEnd(_ text: String) -> String {
        String(text.reversed().drop { $0 == " " || $0 == "," }.reversed())
    }
}
