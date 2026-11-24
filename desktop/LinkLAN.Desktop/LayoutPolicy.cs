namespace LinkLAN.Desktop;

// Coordinates and sizes are WPF DIP. A one-time migration replaces oversized
// legacy widths without preventing later deliberate resizing by the user.
public static class LayoutPolicy
{
    public const double DefaultWidth = 360;
    public const double MinimumWidth = 320;
    public static double RestoreWidth(Preferences preferences) =>
        preferences.WindowLayoutVersion < 1 || preferences.Window.Width < MinimumWidth || preferences.Window.Width > 5000
            ? DefaultWidth : preferences.Window.Width;
}
