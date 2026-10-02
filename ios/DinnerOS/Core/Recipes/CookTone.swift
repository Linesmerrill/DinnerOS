import Foundation
import UserNotifications

/// The sound a finished timer rings, picked by what it's timing: cookies play The Muffin Man,
/// everything else the kitchen chime. All are made in code in
/// the same soft style, so there's no audio file to ship.
nonisolated enum CookTone: String, CaseIterable, Sendable {
    case chime
    case muffinMan

    /// Words in a timer's name that pick a tone ("Chocolate Chip Cookies" → The Muffin Man).
    private static let words: [String: CookTone] = {
        var words: [String: CookTone] = [:]
        for bake in [
            "cookie", "muffin", "cupcake", "brownie", "blondie", "cake", "biscuit", "scone", "shortbread",
        ] {
            words[bake] = .muffinMan
            words[bake + "s"] = .muffinMan
        }
        return words
    }()

    static func `for`(label: String) -> CookTone {
        let names = label.lowercased().split { !$0.isLetter }.map(String.init)
        return names.lazy.compactMap { words[$0] }.first ?? .chime
    }

    /// One loop of the tone as a WAV file.
    var data: Data {
        switch self {
        case .chime: Self.chimeData
        case .muffinMan: Self.muffinManData
        }
    }

    /// The tone for a notification when the app isn't open. Notification sounds are read from
    /// Library/Sounds, so the file is written there the first time it's needed.
    var notificationSound: UNNotificationSound {
        let name = "cook-\(rawValue).wav"
        guard let library = FileManager.default.urls(for: .libraryDirectory, in: .userDomainMask).first else {
            return .default
        }
        let folder = library.appendingPathComponent("Sounds", isDirectory: true)
        let file = folder.appendingPathComponent(name)
        if !FileManager.default.fileExists(atPath: file.path) {
            do {
                try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
                try data.write(to: file, options: .atomic)
            } catch {
                return .default
            }
        }
        return UNNotificationSound(named: UNNotificationSoundName(name))
    }

    // MARK: The tones

    /// Three soft, rising mallet notes that ring out and fade, then a rest: a gentle kitchen
    /// chime rather than a phone alarm. About 2.4 seconds, looped.
    private static let chimeData: Data = {
        var song = Song(seconds: 2.4)
        song.mallet(783.99, at: 0)  // G5
        song.mallet(987.77, at: 0.16)  // B5
        song.mallet(1174.66, at: 0.32, length: 1.4)  // D6, left to ring
        return song.wav
    }()

    /// "Do you know the muffin man" on the same mallet, a little brighter, like a music box. The
    /// tune is a traditional nursery rhyme. About 3 seconds, looped.
    private static let muffinManData: Data = {
        var song = Song(seconds: 3.0)
        let g5 = 783.99, a5 = 880.0, b5 = 987.77, d5 = 587.33
        for (frequency, start) in [(d5, 0.0), (g5, 0.22), (g5, 0.44), (a5, 0.78), (b5, 0.96), (g5, 1.18)] {
            song.mallet(frequency, at: start, bright: true)
        }
        song.mallet(g5, at: 1.40, length: 1.4, bright: true)  // "man", left to ring
        return song.wav
    }()
}

/// A few seconds of mono 16-bit audio built note by note.
private nonisolated struct Song {
    static let rate = 44_100
    var samples: [Double]

    init(seconds: Double) {
        samples = [Double](repeating: 0, count: Int(Double(Self.rate) * seconds))
    }

    private mutating func add(at start: Double, length: Double, _ sample: (Double) -> Double) {
        let first = Int(start * Double(Self.rate))
        for i in 0..<Int(length * Double(Self.rate)) where first + i < samples.count {
            samples[first + i] += sample(Double(i) / Double(Self.rate))
        }
    }

    /// A struck note: a sine and a quiet octave, a soft 5 ms attack, then an exponential decay.
    /// Bright adds a touch of the third harmonic, like a music box.
    mutating func mallet(_ frequency: Double, at start: Double, length: Double = 0.9, bright: Bool = false) {
        add(at: start, length: length) { t in
            let attack = min(1, t / 0.005)
            let decay = exp(-t * 5.5)
            var wave = sin(2 * .pi * frequency * t) + 0.25 * sin(2 * .pi * frequency * 2 * t)
            if bright { wave += 0.08 * sin(2 * .pi * frequency * 3 * t) }
            return wave * attack * decay * 0.28
        }
    }

    var wav: Data {
        let pcm = samples.map { Int16(max(-1, min(1, $0)) * Double(Int16.max)) }
        var data = Data()
        func append<T: FixedWidthInteger>(_ value: T) {
            withUnsafeBytes(of: value.littleEndian) { data.append(contentsOf: $0) }
        }
        let bytes = pcm.count * 2
        data.append(contentsOf: Array("RIFF".utf8))
        append(UInt32(36 + bytes))
        data.append(contentsOf: Array("WAVE".utf8))
        data.append(contentsOf: Array("fmt ".utf8))
        append(UInt32(16))
        append(UInt16(1))
        append(UInt16(1))
        append(UInt32(Self.rate))
        append(UInt32(Self.rate * 2))
        append(UInt16(2))
        append(UInt16(16))
        data.append(contentsOf: Array("data".utf8))
        append(UInt32(bytes))
        for sample in pcm { append(sample) }
        return data
    }
}
