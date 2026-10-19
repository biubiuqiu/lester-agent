import CoreHaptics
import UIKit

@MainActor enum Haptics {
    private static var supported: Bool { CHHapticEngine.capabilitiesForHardware().supportsHaptics && UIApplication.shared.applicationState == .active }
    static func completed() {
        guard supported else { return }
        let generator = UINotificationFeedbackGenerator(); generator.prepare(); generator.notificationOccurred(.success)
    }
    static func selection() {
        guard supported else { return }
        let generator = UISelectionFeedbackGenerator(); generator.selectionChanged()
    }
}
