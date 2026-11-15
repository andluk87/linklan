using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Interop;

namespace LinkLAN.Desktop;

public sealed class NativeTray : IDisposable
{
    const uint Callback = 0x8001; readonly HwndSource source; readonly Action restore; readonly Func<ContextMenu> menu; readonly uint taskbarCreated; Data data; IntPtr icon; bool disposed; public bool Available { get; private set; }
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] struct Data { public uint Size; public IntPtr Window; public uint Id, Flags, Callback; public IntPtr Icon; [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)] public string Tip; public uint State, StateMask; [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 256)] public string Info; public uint Timeout; [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 64)] public string Title; public uint InfoFlags; public Guid Guid; public IntPtr BalloonIcon; }
    [DllImport("shell32.dll", CharSet = CharSet.Unicode)] static extern bool Shell_NotifyIcon(uint operation, ref Data data);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern uint RegisterWindowMessage(string name);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern IntPtr LoadImage(IntPtr instance, string name, uint type, int x, int y, uint flags);
    [DllImport("user32.dll")] static extern bool DestroyIcon(IntPtr icon);
    [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr window);
    [DllImport("shell32.dll")] static extern int SHQueryUserNotificationState(out int state);
    public NativeTray(Window window, Action restore, Func<ContextMenu> menu, Func<Task> quit)
    {
        this.restore = restore; this.menu = menu; var hwnd = new WindowInteropHelper(window).EnsureHandle(); source = HwndSource.FromHwnd(hwnd); source.AddHook(Hook); taskbarCreated = RegisterWindowMessage("TaskbarCreated"); icon = LoadImage(IntPtr.Zero, Environment.ProcessPath!, 1, 32, 32, 0x10); if (icon == IntPtr.Zero) { [DllImport("shell32.dll", CharSet = CharSet.Unicode)] static extern uint ExtractIconEx(string file, int index, IntPtr[] large, IntPtr small, uint count); IntPtr[] icons = new IntPtr[1]; ExtractIconEx(Environment.ProcessPath!, 0, icons, IntPtr.Zero, 1); icon = icons[0]; }
        data = new Data { Size = (uint)Marshal.SizeOf<Data>(), Window = hwnd, Id = 1, Flags = 1 | 2 | 4, Callback = Callback, Icon = icon, Tip = "LinkLAN", Info = "", Title = "" }; Available = Shell_NotifyIcon(0, ref data);
    }
    IntPtr Hook(IntPtr hwnd, int message, IntPtr wParam, IntPtr lParam, ref bool handled) { if ((uint)message == taskbarCreated) { Available = Shell_NotifyIcon(0, ref data); if (!Available) restore(); } if ((uint)message == Callback) { switch ((uint)lParam) { case 0x202: case 0x203: case 0x405: restore(); break; case 0x205: case 0x7B: SetForegroundWindow(hwnd); var popup = menu(); popup.Placement = PlacementMode.MousePoint; popup.IsOpen = true; break; } handled = true; } return IntPtr.Zero; }
    public void Update(string status) { var tip = "LinkLAN · " + status; data.Tip = tip[..Math.Min(127, tip.Length)]; data.Flags = 4; if (Available && !Shell_NotifyIcon(1, ref data)) { Available = false; restore(); } data.Flags = 1 | 2 | 4; }
    public void Notify(string title, string message, Preferences settings) { if (!Available || !settings.NotifyDesktop) return; if (settings.RespectFocus && SHQueryUserNotificationState(out int state) == 0 && state != 5) return; data.Flags = 16 | 0x40; data.Title = title[..Math.Min(63, title.Length)]; data.Info = message[..Math.Min(255, message.Length)]; data.InfoFlags = 1u | (settings.NotifySound ? 0u : 0x10u) | (settings.RespectFocus ? 0x80u : 0u); Shell_NotifyIcon(1, ref data); data.Flags = 1 | 2 | 4; data.Info = ""; data.Title = ""; }
    public void Dispose() { if (disposed) return; disposed = true; Shell_NotifyIcon(2, ref data); source.RemoveHook(Hook); if (icon != IntPtr.Zero) DestroyIcon(icon); Available = false; }
}
