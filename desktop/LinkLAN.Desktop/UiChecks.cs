using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;

namespace LinkLAN.Desktop;

public static class UiChecks
{
    public static async Task<int> Run(MainShell shell, string[] args)
    {
        int index = Array.IndexOf(args, "--validation-dir"); string path = index >= 0 && index + 1 < args.Length ? Path.GetFullPath(args[index + 1]) : Path.Combine(AppContext.BaseDirectory, "validation"); Directory.CreateDirectory(path); var results = new List<object>(); try
        {
            shell.Window.Show(); await Layout(); Assert(shell.VM.Networks.Count == 50, "large fixture missing"); var first = shell.VM.Networks[0].Children[0]; Assert(shell.VM.Networks[0].Children[1].Name == shell.VM.Networks[0].Children[2].Name, "duplicate-name fixture missing");
            foreach (string theme in new[] { "light", "dark" }) foreach (double scale in new[] { 1.0, 1.25, 1.5, 2.0 }) foreach (int width in new[] { 250, 440 })
            {
                Appearance.Apply(theme, true); shell.VM.RefreshAppearance(); shell.Window.Width = width; shell.Window.Height = 500; var root = (FrameworkElement)shell.Window.Content; root.LayoutTransform = new ScaleTransform(scale, scale); shell.Window.Width = width * scale; shell.Window.Height = 500 * scale; await Layout(); var power = shell.Find<Button>("PowerButton"); power.Focus(); await Layout(); Assert(power.FocusVisualStyle == null, "power has rectangular default focus visual"); Assert(power.ActualWidth == 62 && power.ActualHeight == 62, "power circle stretched");
                for (int n = 0; n < 8; n++) { shell.Window.Width = (width + 30 + n) * scale; await Layout(); shell.Window.Width = width * scale; await Layout(); }
                // Compare the retained frame after resizing with an explicit full repaint at
                // the same final geometry. Scroll extent estimation may legitimately
                // change after the first layout; it is not a stale-pixel artifact.
                await Layout(); var after = Capture(shell.Window);
                Invalidate(root); root.UpdateLayout(); await Layout(); var clean = Capture(shell.Window);
                Assert(after.PixelWidth == clean.PixelWidth && after.PixelHeight == clean.PixelHeight, "resize geometry drift");
                if (!Pixels(after).SequenceEqual(Pixels(clean))) { Save(after, Path.Combine(path, "resize-before.png")); Save(clean, Path.Combine(path, "resize-clean.png")); throw new InvalidOperationException("retained frame differs from full repaint at " + width + " / " + scale); }
                var ips = Descendants<TextBlock>(shell.Tree).Where(t => t.IsVisible && System.Windows.Automation.AutomationProperties.GetAutomationId(t) == "PeerIp").ToArray();
                Assert(ips.Length > 0, "no IP cells rendered"); foreach (var ip in ips) { Assert(ip.Text.Count(c => c == '.') == 3, "invalid IP cell"); Assert(ip.ActualWidth <= 112.5, "IP escaped its reserved column"); }
                var name = $"main-{theme}-{width}-{(int)(scale * 100)}"; Save(after, Path.Combine(path, name + ".png")); results.Add(new { name, passed = true, width, scale });
            }
            ((FrameworkElement)shell.Window.Content).LayoutTransform = Transform.Identity; shell.Window.Width = 540; shell.Window.Height = 600; await shell.Patch(new { language = "en", theme = "light" }); Assert(shell.VM.L["Система"] == "System", "language did not apply"); Assert(ReferenceEquals(first, shell.VM.Networks[0].Children[0]), "refresh recreated peer rows"); await shell.Engine.Call("disconnect"); await shell.Refresh(); Assert(!shell.VM.Connected, "power off did not reach engine"); await shell.Engine.Call("connect", new { networkId = "preview-1" }); await shell.Refresh(); Assert(shell.VM.Connected, "power on did not reach engine"); await shell.Patch(new { language = "ru", theme = "light" });
            for (int page = 0; page < 6; page++) { var dialog = Dialogs.Settings(shell, page); dialog.Show(); await Layout(); Save(Capture((FrameworkElement)dialog.Content), Path.Combine(path, $"settings-{page}.png")); dialog.Close(); }
            File.WriteAllText(Path.Combine(path, "wpf-check.json"), JsonSerializer.Serialize(new { passed = true, framework = "WPF", cases = results, duplicateNames = true, stableRows = true, enginePower = true }, new JsonSerializerOptions { WriteIndented = true })); return 0;
        }
        catch (Exception e) { File.WriteAllText(Path.Combine(path, "wpf-check.json"), JsonSerializer.Serialize(new { passed = false, error = e.ToString(), cases = results }, new JsonSerializerOptions { WriteIndented = true })); return 1; }
    }
    static IEnumerable<T> Descendants<T>(DependencyObject root) where T : DependencyObject { for (int i = 0; i < VisualTreeHelper.GetChildrenCount(root); i++) { var child = VisualTreeHelper.GetChild(root, i); if (child is T item) yield return item; foreach (var nested in Descendants<T>(child)) yield return nested; } }
    static void Invalidate(DependencyObject root) { if (root is UIElement element) { element.InvalidateMeasure(); element.InvalidateArrange(); element.InvalidateVisual(); } for (int i = 0; i < VisualTreeHelper.GetChildrenCount(root); i++) Invalidate(VisualTreeHelper.GetChild(root, i)); }
    static void Assert(bool condition, string message) { if (!condition) throw new InvalidOperationException(message); }
    static async Task Layout() { await Application.Current.Dispatcher.InvokeAsync(() => { }, DispatcherPriority.ApplicationIdle); await Task.Delay(60); }
    static RenderTargetBitmap Capture(FrameworkElement element) { element.UpdateLayout(); var bitmap = new RenderTargetBitmap(Math.Max(1, (int)Math.Ceiling(element.ActualWidth)), Math.Max(1, (int)Math.Ceiling(element.ActualHeight)), 96, 96, PixelFormats.Pbgra32); bitmap.Render(element); return bitmap; }
    static byte[] Pixels(BitmapSource source) { var pixels = new byte[source.PixelWidth * source.PixelHeight * 4]; source.CopyPixels(pixels, source.PixelWidth * 4, 0); return pixels; }
    static void Save(BitmapSource image, string path) { using var stream = File.Create(path); var encoder = new PngBitmapEncoder(); encoder.Frames.Add(BitmapFrame.Create(image)); encoder.Save(stream); }
}
