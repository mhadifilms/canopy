import Foundation

/// Formats dates as relative time strings for the session list.
///
/// Examples: "just now", "2m ago", "1h ago", "yesterday", "Feb 20"
enum RelativeDateFormatter {

    /// Format a date relative to now for display in session rows.
    static func string(for date: Date, relativeTo now: Date = Date()) -> String {
        let interval = now.timeIntervalSince(date)

        if interval < 0 {
            return "just now"
        }

        let seconds = Int(interval)
        let minutes = seconds / 60
        let hours = minutes / 60
        let days = hours / 24

        switch seconds {
        case 0..<60:
            return "just now"
        case 60..<3600:
            return "\(minutes)m ago"
        case 3600..<86400:
            return "\(hours)h ago"
        default:
            break
        }

        let calendar = Calendar.current

        if calendar.isDateInYesterday(date) {
            return "yesterday"
        }

        if days < 7 {
            return "\(days)d ago"
        }

        // Fall back to short date
        let formatter = DateFormatter()
        if calendar.component(.year, from: date) == calendar.component(.year, from: now) {
            formatter.dateFormat = "MMM d"
        } else {
            formatter.dateFormat = "MMM d, yyyy"
        }
        return formatter.string(from: date)
    }

    /// Format a duration in milliseconds for display on completed badges.
    /// Examples: "< 1s", "3s", "45s", "2m 15s", "1h 5m"
    static func duration(ms: Int) -> String {
        let totalSeconds = ms / 1000

        if totalSeconds < 1 {
            return "< 1s"
        }

        let hours = totalSeconds / 3600
        let minutes = (totalSeconds % 3600) / 60
        let seconds = totalSeconds % 60

        if hours > 0 {
            return "\(hours)h \(minutes)m"
        }
        if minutes > 0 {
            return "\(minutes)m \(seconds)s"
        }
        return "\(seconds)s"
    }
}
