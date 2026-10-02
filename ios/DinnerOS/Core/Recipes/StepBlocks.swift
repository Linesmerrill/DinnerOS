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
    static let minimumItems = 2

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
            if let (list, after) = list(sentence) {
                if !prose.isEmpty { out.append(.prose(prose)) }
                prose = after
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

    /// Just past a sentence's end and the spaces after it: ". " or a final "." (the next segment
    /// starts a new sentence).
    private static func sentenceEnd(in text: Substring) -> Substring.Index? {
        var i = text.startIndex
        while i < text.endIndex {
            var next = text.index(after: i)
            // A closing bracket or quote stays with its sentence: "…later.) Season".
            if ".!?;".contains(text[i]) {
                while next < text.endIndex, ")\"”’".contains(text[next]) { next = text.index(after: next) }
            }
            if ".!?;".contains(text[i]), next == text.endIndex || text[next] == " " {
                var end = next
                while end < text.endIndex, text[end] == " " { end = text.index(after: end) }
                return end
            }
            i = next
        }
        return nil
    }

    /// What joins two names in a list: any part words that finish the first name ("scallion
    /// whites"), a comma, "and", or "with" ("combine sour cream with spice blend"), then at most a few
    /// words that belong to the next name ("a pinch of", "juice from", "another large drizzle of").
    private static let joinRe = try? NSRegularExpression(
        pattern:
            #"^((?:\s+[\p{L}’'-]+){0,2}?)(?:\s*,\s*(?:(?:and|or)\s+)?|\s+(?:and|or|with)\s+|^(?:and|or|with)\s+)((?:[\p{L}’'-]+\s+){0,4})$"#
    )

    /// Words after a name that are still that name: "scallion whites", "lemon zest", "lime juice".
    private static let partWords: Set<String> = [
        "white", "whites", "green", "greens", "zest", "juice", "wedge", "wedges", "leaves", "stems", "florets",
        "halves", "slices", "pieces", "strips", "rounds", "cubes", "mixture", "filling",
    ]

    /// Words that start a new part of the sentence rather than describe the next name: "and into the
    /// pan with chicken" isn't a list.
    private static let clauseWords: Set<String> = [
        "into", "with", "to", "in", "on", "onto", "over", "then", "until", "for", "between", "among", "across", "and",
        "or",
        "around", "evenly",
    ]

    /// Verbs that start the next thing to do after a list: "…and serve", ", then divide…".
    private static let actionVerbs: Set<String> = [
        "serve", "divide", "cook", "stir", "toss", "transfer", "bring", "simmer", "bake", "roast", "let", "season",
        "top", "garnish", "drizzle", "sprinkle", "remove", "place", "return", "turn", "reduce", "cover", "keep",
        "add", "set", "spoon", "pour", "fold", "mash", "whisk", "taste",
    ]

    /// The rest of the sentence as its own line, when it's the next thing to do: " and serve." →
    /// "Serve."; `nil` otherwise.
    private static func nextAction(_ tail: String) -> String? {
        var rest = Substring(tail).drop { $0 == " " || $0 == "," }
        for joiner in ["and then ", "then ", "and "] where rest.lowercased().hasPrefix(joiner) {
            rest = rest.dropFirst(joiner.count)
            let word = rest.prefix { $0.isLetter }.lowercased()
            guard actionVerbs.contains(word), let first = rest.first else { return nil }
            return first.uppercased() + rest.dropFirst()
        }
        return nil
    }

    /// Words a list's lead ends on: what's done, or where it goes.
    private static let leadEnds: Set<String> = [
        "combine", "mix", "whisk", "stir", "add", "toss", "season", "heat", "place", "sprinkle", "top", "garnish",
        "divide", "fill", "drizzle", "serve", "melt", "pour", "spoon", "scatter", "arrange", "transfer", "coat",
        "rub", "brush", "dollop", "layer", "return", "put", "cook", "bring", "simmer", "warm", "boil", "together",
        "in", "with", "into", "on", "onto", "over", "blend", "fold", "toast", "roast", "bake", "saute", "sauté",
        "use", "keep", "set", "reserve", "measure", "plate", "drain", "rinse", "chop", "slice", "mince", "dice",
        "halve", "peel", "trim", "zest", "quarter", "wash", "grate", "shred", "crush", "squeeze", "juice", "tear",
    ]

    /// Verbs that never lead a name, so ", add sour cream" starts a new list rather than continuing one.
    /// ("a drizzle of", "a squeeze of", "juice from" do lead names.)
    private static let joinVerbs: Set<String> = [
        "add", "stir", "toss", "season", "top", "cook", "serve", "divide", "place", "transfer", "combine", "mix",
        "whisk", "pour", "spoon", "sprinkle", "garnish", "fill", "bring", "return", "remove", "cover", "reduce",
        "let", "keep", "heat", "melt", "simmer",
    ]

    /// Verbs that mix what they name, so "with" joins two things going in together.
    private static let mixingVerbs: Set<String> = ["combine", "mix", "whisk", "stir", "together", "blend"]

    /// Words that start a sentence about something other than what goes in.
    private static let subordinators: Set<String> = [
        "once", "while", "when", "until", "if", "after", "before", "as", "keep", "meanwhile", "serve",
    ]

    /// How a gap joins two names: the part words that finish the first, and the words that start the
    /// second; `nil` when the gap isn't a list's join.
    private static func join(_ text: String) -> (part: String, words: String)? {
        let bracket = tailPart(text)
        if bracket.part.hasSuffix(")"), let rest = join(bracket.rest) {
            return (bracket.part + rest.part, rest.words)
        }
        guard let joinRe else { return nil }
        let ns = text as NSString
        guard let m = joinRe.firstMatch(in: text, range: NSRange(location: 0, length: ns.length)) else { return nil }
        let part = ns.substring(with: m.range(at: 1)), words = ns.substring(with: m.range(at: 2))
        guard part.lowercased().split(separator: " ").allSatisfy({ partWords.contains(String($0)) }),
            !words.lowercased().split(separator: " ").contains(where: {
                clauseWords.contains(String($0)) || joinVerbs.contains(String($0))
            })
        else { return nil }
        return (part, words)
    }

    /// The part words right after the last name ("scallion whites; cook"), split from the rest.
    private static func tailPart(_ tail: String) -> (part: String, rest: String) {
        var part = "", rest = Substring(tail)
        // "pickled onion (draining first)": a bracket right after a name is that name's.
        if let close = rest.firstIndex(of: ")"), rest.drop(while: { $0 == " " }).first == "(",
            rest[..<close].filter({ $0 == "(" }).count == 1
        {
            part = String(rest[...close])
            rest = rest[rest.index(after: close)...]
        }
        while true {
            let spaces = rest.prefix { $0 == " " }
            let word = rest.dropFirst(spaces.count).prefix { $0.isLetter }
            guard !spaces.isEmpty, partWords.contains(word.lowercased()) else { break }
            part += spaces + word
            rest = rest.dropFirst(spaces.count + word.count)
        }
        return (part, String(rest))
    }

    /// The sentence as a list, when it adds at least `minimumItems` things in a row after a lead.
    /// With what follows the list in the same sentence ("…and serve." reads "Serve." under it).
    private static func list(_ sentence: [InstructionSegment]) -> (StepBlock, [InstructionSegment])? {
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
        // A run whose lead doesn't read ("On the sheet from pork, toss green beans with…" starts at
        // pork) is tried again from its next name.
        guard var run = best else { return nil }
        while run.count >= minimumItems {
            if let found = list(sentence, names: names, run: run) { return found }
            run = (run.lowerBound + 1)..<run.upperBound
        }
        return nil
    }

    private static func list(_ sentence: [InstructionSegment], names: [Int], run: Range<Int>)
        -> (StepBlock, [InstructionSegment])?
    {
        let first = names[run.lowerBound], last = names[run.upperBound - 1]
        var lead = Array(sentence[..<first])
        var items: [[InstructionSegment]] = [firstItemWords(&lead) + [sentence[first]]]
        guard lead.contains(where: { !$0.isIngredient && !$0.text.trimmingCharacters(in: .whitespaces).isEmpty })
        else { return nil }
        // "Once rice and beans are done, stir…" names its subject, not what goes in; and a sentence
        // that goes on to another clause after the names ("…and pepper, then roast") reads as written.
        // "While rice cooks, in a bowl, combine…" is fine: the comma closes the "while" before the list.
        let leadText = lead.map(\.text).joined().lowercased()
        let firstWord = leadText.split(whereSeparator: { !$0.isLetter }).first
        guard let firstWord, !subordinators.contains(String(firstWord)) || leadText.contains(",") else { return nil }
        // The lead ends on what's done ("combine", "season with", "stir in"); one that ends on a name
        // the recipe doesn't list ("combine BBQ sauce, mustard") isn't a list's lead.
        guard let lastWord = leadText.split(whereSeparator: { !$0.isLetter }).last, leadEnds.contains(String(lastWord))
        else { return nil }
        // …nor one that still lists names ("Add turkey, 1 Tbsp spice blend, salt, and pepper" from salt).
        let listInLead = lead.indices.contains { i in
            lead[i].isIngredient && i + 1 < lead.count && join(lead[i + 1].text)?.words.isEmpty == true
        }
        guard !listInLead, leadText.filter({ $0 == "(" }).count == leadText.filter({ $0 == ")" }).count
        else { return nil }
        for k in (run.lowerBound + 1)..<run.upperBound {
            let gap = sentence[(names[k - 1] + 1)..<names[k]].map(\.text).joined()
            let (part, words) = join(gap) ?? ("", "")
            if !part.isEmpty { items[items.count - 1].append(InstructionSegment(kind: .text, text: part)) }
            items.append(
                (words.isEmpty ? [] : [InstructionSegment(kind: .text, text: words)]) + [sentence[names[k]]])
        }
        // "Season pasta with salt and pepper": with most verbs, what comes before "with" is what's
        // being worked on, so it stays in the lead ("Season pasta with:"). "Combine sour cream with
        // spice blend" mixes them, so both are on the list.
        let verb = lead.map(\.text).joined().lowercased().split(whereSeparator: { !$0.isLetter }).last.map(String.init)
        // The first "with" in the run; everything before it is what the verb works on.
        let withAt = ((run.lowerBound + 1)..<run.upperBound).first { k in
            let words = sentence[(names[k - 1] + 1)..<names[k]].map(\.text).joined().lowercased()
                .split(whereSeparator: { !$0.isLetter }).map(String.init)
            return words.drop(while: { partWords.contains($0) }).first == "with"
        }
        let withObject = withAt != nil && !mixingVerbs.contains(verb ?? "") || verb == "with"
        if let withAt, withObject {
            let gap = sentence[names[withAt] - 1].text
            let upToWith = gap.range(of: "with").map { String(gap[..<$0.upperBound]) } ?? gap
            lead +=
                items[0].prefix { !$0.isIngredient } + sentence[first..<(names[withAt] - 1)]
                + [InstructionSegment(kind: .text, text: upToWith)]
            items.removeFirst(withAt - run.lowerBound)
        }
        // "…and scallion whites; cook" ends on part words that finish the last name.
        var tailSegments = Array(sentence[(last + 1)...])
        if let i = tailSegments.indices.first, !tailSegments[i].isIngredient {
            let (part, rest) = tailPart(tailSegments[i].text)
            if !part.isEmpty {
                items[items.count - 1].append(InstructionSegment(kind: .text, text: part))
                tailSegments[i] = InstructionSegment(kind: .text, text: rest)
            }
        }
        // "as many chili flakes as you like" is one item, not a lead's qualifier.
        let tailText0 = tailSegments.map(\.text).joined()
        if let lastItem = items.last?.first?.text.lowercased(),
            lastItem.hasPrefix("as many") || lastItem.hasPrefix("as much"),
            !tailSegments.contains(where: \.isIngredient),
            let phrase = ["as you like", "as desired", "as needed"].first(where: {
                tailText0.trimmingCharacters(in: .whitespaces).lowercased().hasPrefix($0)
            })
        {
            let start = tailText0.drop { $0 == " " }
            items[items.count - 1].append(InstructionSegment(kind: .text, text: " " + start.prefix(phrase.count)))
            let rest = String(start.dropFirst(phrase.count))
            tailSegments = rest.isEmpty ? [] : [InstructionSegment(kind: .text, text: rest)]
        }
        // "…, ¾ cup water, and a big pinch of salt." ends on one more thing the recipe doesn't list.
        if let more = lastPlainItem(tailSegments) {
            items.append([InstructionSegment(kind: .text, text: more.item)])
            tailSegments = more.rest.isEmpty ? [] : [InstructionSegment(kind: .text, text: more.rest)]
        }
        // "…and serve." / ", then divide between bowls." is the next thing to do: it goes under the list.
        var after: [InstructionSegment] = []
        if let i = tailSegments.indices.first, !tailSegments[i].isIngredient,
            let next = nextAction(tailSegments[i].text)
        {
            after = [InstructionSegment(kind: .text, text: next)] + tailSegments.dropFirst()
            tailSegments = []
        }
        let tailText = tailSegments.map(\.text).joined()
        let tailWordsLeft = tailText.trimmingCharacters(in: .whitespaces.union(.punctuationCharacters)).lowercased()
        guard items.count >= minimumItems, !tailText.contains(","),
            !tailWordsLeft.hasPrefix("and "), !tailWordsLeft.hasPrefix("or "), !tailText.contains("("),
            // "Toss steak with cornstarch, salt, and pepper until coated" reads as written.
            !withObject || tailWordsLeft.isEmpty || qualifiers.contains(tailWordsLeft)
        else { return nil }
        let line = leadLine(lead, tail: tailSegments)
        let lineText = line.map(\.text).joined()
        guard lineText.filter({ $0 == "(" }).count == lineText.filter({ $0 == ")" }).count else { return nil }
        return (.list(lead: line, items: items), after)
    }

    private static let plainStarts: Set<String> = ["a", "an", "some", "another", "extra", "more"]
    private static let plainNames: Set<String> = ["salt", "pepper", "water", "oil"]

    private static let lastPlainRe = try? NSRegularExpression(
        pattern: #"^(?:\s*,\s*(?:and|or)\s+|\s+(?:and|or)\s+)([^,.;:()\d]+?)\s*([.;!?]?\s*)$"#)

    /// One more thing at the end that the recipe doesn't list ("…, and a big pinch of salt."), with
    /// what's left after it; `nil` when the end says something else.
    private static func lastPlainItem(_ tail: [InstructionSegment]) -> (item: String, rest: String)? {
        guard !tail.contains(where: \.isIngredient), let lastPlainRe else { return nil }
        let text = tail.map(\.text).joined()
        let ns = text as NSString
        guard let m = lastPlainRe.firstMatch(in: text, range: NSRange(location: 0, length: ns.length)) else {
            return nil
        }
        let item = ns.substring(with: m.range(at: 1))
        let words = item.lowercased().split(separator: " ")
        // It has to read as an amount of something: "a big pinch of salt", "some pepper", "salt".
        guard words.count <= 6, !words.contains(where: { clauseWords.contains(String($0)) }),
            let first = words.first,
            plainStarts.contains(String(first))
                || words.count <= 2 && words.contains(where: { plainNames.contains(String($0)) })
        else { return nil }
        return (item, ns.substring(with: m.range(at: 2)))
    }

    /// Words at the end of the lead that belong to the first name ("remaining", "¼ of the", "a pinch
    /// of"), taken off the lead: "Season with remaining" reads "Season with:" over "remaining Paprika".
    private static let firstItemRe = try? NSRegularExpression(
        pattern:
            #"(?i)(?:^|(?<=\s))((?:the\s+)?remaining\s+|half(?:\s+of)?(?:\s+the)?\s+|plenty\s+of\s+|[\d½¼¾⅓⅔⅛/]+\s+of(?:\s+the)?\s+|a\s+(?:(?:big|large|small)\s+)?(?:pinch|drizzle|splash|squeeze)\s+of\s+|(?:juice|zest)\s+from\s+|(?:the\s+)?(?:(?:thinly|finely|roughly)\s+)?(?:sliced|drained|minced|chopped|diced|grated|shredded|halved|crushed|melted|softened|rinsed|cooked|reserved|toasted|roasted|quartered|torn|beaten|thawed|crumbled|smashed|mashed|pickled|seasoned|sautéed|browned)\s+|the\s+)$"#
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
        if qualifiers.contains(tailText.lowercased()) {
            // "Season with salt and pepper to taste" reads "Season with (to taste):".
            out.append(InstructionSegment(kind: .text, text: " (" + tailText + ")"))
        } else if !tailText.isEmpty {
            if let i = tail.indices.first, !tail[i].isIngredient, !tail[i].text.hasPrefix(" ") {
                tail[i] = InstructionSegment(kind: .text, text: " " + tail[i].text)
            }
            out += tail
        }
        // A sentence that began after a ";" starts the lead's line, so it starts with a capital.
        if let i = out.indices.first, !out[i].isIngredient, let first = out[i].text.first, first.isLowercase {
            out[i] = InstructionSegment(kind: .text, text: first.uppercased() + out[i].text.dropFirst())
        }
        return out + [InstructionSegment(kind: .text, text: ":")]
    }

    /// How much, said after the names: kept on the lead in brackets.
    private static let qualifiers: Set<String> = [
        "to taste", "if desired", "as you like", "as desired", "as needed", "to your liking",
    ]

    private static func trimmedEnd(_ text: String) -> String {
        String(text.reversed().drop { $0 == " " || $0 == "," }.reversed())
    }
}
