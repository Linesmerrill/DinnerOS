import AVFoundation
import Foundation

/// The kitchen-timer alarm: a soft tone (`CookTone`) that repeats while any timer is finished. There
/// is one, shared by every timer, so three timers ending together ring once, not three times over;
/// the first to finish picks the tone.
///
/// It plays as media (`.playback`), so it sounds with the ringer switch off, the way a kitchen
/// timer should, and stops by itself after a minute.
@MainActor
final class CookAlarm {
    private var player: AVAudioPlayer?
    private var stopTask: Task<Void, Never>?

    var isRinging: Bool { player?.isPlaying == true }

    func ring(_ tone: CookTone = .chime) {
        guard !isRinging else { return }
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, options: [.duckOthers])
            try session.setActive(true)
            let player = try AVAudioPlayer(data: tone.data)
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
}
