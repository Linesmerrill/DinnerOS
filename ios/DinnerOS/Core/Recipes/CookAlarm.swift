import AVFoundation
import Foundation

/// The kitchen-timer alarm: a soft three-note chime that repeats while any timer is finished. There is
/// one, shared by every timer, so three timers ending together ring once, not three times over.
///
/// It plays as media (`.playback`), so it sounds with the ringer switch off, the way a kitchen
/// timer should, and stops by itself after a minute.
@MainActor
final class CookAlarm {
    private var player: AVAudioPlayer?
    private var stopTask: Task<Void, Never>?

    var isRinging: Bool { player?.isPlaying == true }

    func ring() {
        guard !isRinging else { return }
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, options: [.duckOthers])
            try session.setActive(true)
            let player = try AVAudioPlayer(data: Self.chime)
            player.numberOfLoops = -1
            player.volume = 0.8
            player.play()
            self.player = player
        } catch {
            return
        }
        stopTask?.cancel()
        stopTask = Task { @MainActor [weak self] in
            do { try await Task.sleep(for: .seconds(60)) } catch { return }
            self?.stop()
        }
    }

    func stop() {
        stopTask?.cancel()
        stopTask = nil
        player?.stop()
        player = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// Three soft, rising mallet notes that ring out and fade, then a rest: a gentle kitchen
    /// chime rather than a phone alarm. About 2.4 seconds, looped. Built in code so there's no
    /// audio file to ship.
    static let chime: Data = {
        let rate = 44_100
        var samples = [Double](repeating: 0, count: Int(Double(rate) * 2.4))
        // A mallet note: a sine and a quiet octave, a soft 5 ms attack, then an exponential
        // decay, so it sounds struck rather than beeped.
        func note(_ frequency: Double, at start: Double, length: Double = 0.9) {
            let first = Int(start * Double(rate))
            for i in 0..<Int(length * Double(rate)) where first + i < samples.count {
                let t = Double(i) / Double(rate)
                let attack = min(1, t / 0.005)
                let decay = exp(-t * 5.5)
                let wave = sin(2 * .pi * frequency * t) + 0.25 * sin(2 * .pi * frequency * 2 * t)
                samples[first + i] += wave * attack * decay * 0.28
            }
        }
        note(783.99, at: 0)  // G5
        note(987.77, at: 0.16)  // B5
        note(1174.66, at: 0.32, length: 1.4)  // D6, left to ring
        let pcm = samples.map { Int16(max(-1, min(1, $0)) * Double(Int16.max)) }

        var data = Data()
        func append<T: FixedWidthInteger>(_ value: T) {
            withUnsafeBytes(of: value.littleEndian) { data.append(contentsOf: $0) }
        }
        let bytes = pcm.count * 2
        data.append(contentsOf: Array("RIFF".utf8)); append(UInt32(36 + bytes))
        data.append(contentsOf: Array("WAVE".utf8)); data.append(contentsOf: Array("fmt ".utf8))
        append(UInt32(16)); append(UInt16(1)); append(UInt16(1)); append(UInt32(rate))
        append(UInt32(rate * 2)); append(UInt16(2)); append(UInt16(16))
        data.append(contentsOf: Array("data".utf8)); append(UInt32(bytes))
        for sample in pcm { append(sample) }
        return data
    }()
}
