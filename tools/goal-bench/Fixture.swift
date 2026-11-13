import UIKit

// Synthetic, resettable evaluation app. Each launch owns only in-memory state.
// No network, accounts, persistent data or permissions are used.
@main
final class FixtureDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?
    func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        let window = UIWindow(frame: UIScreen.main.bounds)
        window.rootViewController = FixtureController()
        window.makeKeyAndVisible()
        self.window = window
        return true
    }
}

final class FixtureController: UIViewController {
    let scenario = ProcessInfo.processInfo.arguments.dropFirst().first ?? "direct"
    let number = ProcessInfo.processInfo.arguments.dropFirst(2).first ?? "01"
    var field: UITextField?
    var heading: UILabel?
    weak var scrollView: UIScrollView?

    override func viewDidLoad() {
        super.viewDidLoad()
        NotificationCenter.default.addObserver(self, selector: #selector(keyboardChanged(_:)), name: UIResponder.keyboardWillChangeFrameNotification, object: nil)
        show("Home")
    }

    func show(_ title: String) {
        view.subviews.forEach { $0.removeFromSuperview() }
        view.backgroundColor = .systemBackground
        view.accessibilityIdentifier = title.replacingOccurrences(of: " ", with: "") + "View"
        let scroll = UIScrollView()
        scroll.accessibilityIdentifier = "content"
        scroll.frame = view.bounds.inset(by: UIEdgeInsets(top: 70, left: 24, bottom: 40, right: 24))
        scroll.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(scroll)
        scrollView = scroll
        let stack = UIStackView()
        stack.axis = .vertical
        stack.spacing = 18
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor),
            stack.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor),
            stack.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor),
            stack.widthAnchor.constraint(equalTo: scroll.frameLayoutGuide.widthAnchor)
        ])
        let heading = UILabel()
        heading.text = title
        heading.font = .preferredFont(forTextStyle: .largeTitle)
        heading.accessibilityTraits = .header
        heading.accessibilityIdentifier = "heading"
        self.heading = heading
        stack.addArrangedSubview(heading)

        func button(_ name: String, _ action: @escaping () -> Void) {
            let b = UIButton(type: .system)
            b.setTitle(name, for: .normal)
            b.accessibilityIdentifier = name.lowercased().replacingOccurrences(of: " ", with: "_")
            b.heightAnchor.constraint(equalToConstant: 52).isActive = true
            b.addAction(UIAction { _ in action() }, for: .touchUpInside)
            stack.addArrangedSubview(b)
        }

        if title == "Saved" || title == "Details " + number { return }
        if ["form", "false-final", "focus-loss"].contains(scenario) {
            // The keyboard forces a scroll/re-layout rather than leaving
            // every useful control at a permanently convenient coordinate.
            for i in 1...3 {
                let label = UILabel()
                label.text = "Form section \(i)"
                label.heightAnchor.constraint(equalToConstant: 90).isActive = true
                stack.addArrangedSubview(label)
            }
            let field = UITextField()
            field.borderStyle = .roundedRect
            field.placeholder = "Note text"
            field.accessibilityLabel = "Note text"
            field.accessibilityIdentifier = "note"
            field.heightAnchor.constraint(equalToConstant: 52).isActive = true
            field.addTarget(self, action: #selector(textChanged), for: .editingChanged)
            if scenario == "focus-loss" { field.addTarget(self, action: #selector(loseFocus), for: .editingDidBegin) }
            self.field = field
            stack.addArrangedSubview(field)
            button("Save note") { [weak self] in
                guard let self, self.field?.text == "Sample " + self.number else { return }
                self.view.endEditing(true)
                if self.scenario == "form" { self.show("Saved") }
                else { self.heading?.accessibilityValue = "Save request reported; completion absent" }
            }
        } else if scenario == "transition" && title == "Home" {
            button("Prepare navigation") { [weak self] in self?.show("Transition") }
        } else if scenario == "multi" && title == "Home" {
            button("Projects") { [weak self] in self?.show("Projects") }
            button("Archive") { [weak self] in self?.show("Archive") }
        } else {
            if scenario == "scroll" {
                for i in 1...20 { button("Other item \(i)") {} }
            }
            if scenario != "absent" {
                button("Details " + number) { [weak self] in
                    if self?.scenario == "app-change", let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
                    else { self?.show("Details " + (self?.number ?? "")) }
                }
            }
            button("Help") { [weak self] in self?.show("Help") }
        }
        if scenario == "transition" && title == "Transition" {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
                if self?.heading?.text == "Transition" { self?.show("Moved") }
            }
        }
        if scenario == "modal" && title == "Home" {
            DispatchQueue.main.async { [weak self] in
                let alert = UIAlertController(title: "Permission required", message: "Synthetic modal", preferredStyle: .alert)
                alert.addAction(UIAlertAction(title: "Decline", style: .cancel))
                alert.addAction(UIAlertAction(title: "Allow", style: .default))
                self?.present(alert, animated: false)
            }
        }
    }

    @objc func textChanged() { field?.accessibilityValue = field?.text }
    @objc func loseFocus() {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) { [weak self] in self?.field?.resignFirstResponder() }
    }

    @objc func keyboardChanged(_ note: Notification) {
        guard let scroll = scrollView, let frame = note.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? CGRect else { return }
        let keyboard = view.convert(frame, from: nil)
        scroll.contentInset.bottom = max(0, view.bounds.maxY - keyboard.minY)
        if let field, field.isFirstResponder, field.isDescendant(of: scroll) {
            scroll.scrollRectToVisible(field.convert(field.bounds, to: scroll).insetBy(dx: 0, dy: -50), animated: false)
        }
    }
}
