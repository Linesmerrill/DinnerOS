import AVFoundation
import Foundation
import Testing

@testable import DinnerOS

/// Timers are named for what they're timing, and the alarm is a real, playable sound.
@MainActor
struct CookTimerNamingTests {
    @Test func aTimeLinkCarriesWhatItsFor() throws {
        let durations = CookDurations.find(in: "until zucchini is tender, 2-3 minutes.")
        let url = try #require(CookTimerText.url(step: 3, duration: durations[0], subject: "Zucchini"))
        let request = try #require(CookTimerText.request(from: url))
        #expect(request.subject == "Zucchini")
        #expect(request.lowSeconds == 120)
        #expect(request.highSeconds == 180)
    }

    @Test func aCookedThingInTheSentenceNamesTheTimer() {
        #expect(CookTimerSubject.noun(in: "Add pasta to the pot and cook until al dente, ") == "Pasta")
        #expect(CookTimerSubject.noun(in: "Wash the produce. Toast the tortillas, ") == "Tortillas")
        #expect(CookTimerSubject.noun(in: "Stir until thick, ") == nil)
        // A word inside another word doesn't count.
        #expect(CookTimerSubject.noun(in: "Add the ricotta, ") == nil)
    }

    @Test func everyToneIsAPlayableSound() throws {
        for tone in CookTone.allCases {
            let player = try AVAudioPlayer(data: tone.data)
            #expect(player.duration > 2 && player.duration < 3.5, "\(tone)")
        }
    }

    @Test func whatsTimedPicksTheTone() {
        #expect(CookTone.for(label: "Chocolate Chip Cookies") == .muffinMan)
        #expect(CookTone.for(label: "Blueberry Muffins") == .muffinMan)
        #expect(CookTone.for(label: "Organic Chicken Cutlets") == .quest)
        #expect(CookTone.for(label: "Zucchini") == .chime)
        #expect(CookTone.for(label: "Step 2") == .chime)
        // A word inside another word doesn't count.
        #expect(CookTone.for(label: "Pancakes") == .chime)
    }
}

@MainActor
struct CookTimerOrderTests {
    @Test func dragMovesATimerToAnothersPlace() {
        let timers = CookTimers()
        let a = timers.start(label: "Pasta", seconds: 600)
        let b = timers.start(label: "Sauce", seconds: 300)
        let c = timers.start(label: "Bread", seconds: 120)
        timers.move(c.id, to: a.id)
        #expect(timers.timers.map(\.label) == ["Bread", "Pasta", "Sauce"])
        timers.move(c.id, to: b.id)
        #expect(timers.timers.map(\.label) == ["Pasta", "Sauce", "Bread"])
        timers.removeAll()
    }
}

@MainActor
struct CookTimerMinuteTests {
    @Test func aMinuteComesOffButNeverFinishesTheTimer() {
        var clock = Date(timeIntervalSince1970: 1_000_000)
        let timers = CookTimers(now: { clock })
        let t = timers.start(label: "Veggies", seconds: 300)
        timers.removeMinute(t.id)
        #expect(timers.timers[0].remaining(at: clock) == 240)
        #expect(timers.timers[0].total == 240)
        // With a minute or less left, taking one off does nothing.
        clock = clock.addingTimeInterval(190)
        timers.removeMinute(t.id)
        #expect(timers.timers[0].remaining(at: clock) == 50)
        // Paused, the minute comes off what's left.
        timers.addMinute(t.id)
        timers.pause(t.id)
        timers.removeMinute(t.id)
        #expect(timers.timers[0].remaining(at: clock) == 50)
        timers.removeAll()
    }
}

struct CookTimerSubjectTests {
    @Test func namesTheCookingNotTheSeasoning() {
        let names = ["Ground Beef", "Salt", "Shallot", "Rigatoni Pasta"]
        #expect(
            CookTimerSubject.pick(
                sentence: "Add beef and shallot, season with salt. Cook, breaking up meat into pieces, until browned, ",
                ingredients: names) == "Ground Beef")
        #expect(
            CookTimerSubject.pick(
                sentence: "Add rigatoni to pot. Cook, stirring occasionally, until al dente, ", ingredients: names)
                == "Rigatoni Pasta")
        #expect(CookTimerSubject.pick(sentence: "Season with salt and cook, ", ingredients: ["Salt"]) == nil)
    }
}
