import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow {
  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    self.contentViewController = flutterViewController

    // AniSync is a two-pane tool: rule editor on the left, plan table on the
    // right. 1280x800 is the comfortable default; contentMinSize is the point
    // below which the table starts clipping its columns. contentMinSize (not
    // minSize) is deliberate -- it constrains the Flutter view, matching the
    // client-area minimum used by the Windows runner.
    self.setContentSize(NSSize(width: 1280, height: 800))
    self.contentMinSize = NSSize(width: 1024, height: 680)
    self.center()

    RegisterGeneratedPlugins(registry: flutterViewController)

    super.awakeFromNib()
  }
}
