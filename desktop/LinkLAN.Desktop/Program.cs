using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Security.Principal;
using System.Text;
using System.Windows;
using System.Windows.Threading;

namespace LinkLAN.Desktop;

public static class Program
{
    static readonly string Identity = WindowsIdentity.GetCurrent().User?.Value ?? Environment.UserName;
    static readonly string PipeName = "LinkLAN.WPF." + Identity;
    static Mutex? singleton;
    [STAThread]
    public static int Main(string[] args)
    {
        if (args.Contains("--version")) { Console.WriteLine(typeof(Program).Assembly.GetName().Version?.ToString(3)); return 0; }
        if (args.Contains("--quit")) return QuitExisting();
        if (args.Contains("--autostart") || args.Contains("--headless") || args.Contains("--self-test")) return Forward(args);
        bool check = args.Contains("--ui-check"), preview = check || args.Any(a => a.StartsWith("--ui-preview", StringComparison.Ordinal));
        if (!preview && !new WindowsPrincipal(WindowsIdentity.GetCurrent()).IsInRole(WindowsBuiltInRole.Administrator))
        {
            try { var start = new ProcessStartInfo(Environment.ProcessPath!) { UseShellExecute = true, Verb = "runas" }; foreach (var arg in args) start.ArgumentList.Add(arg); Process.Start(start); return 0; } catch { return 1; }
        }
        if (!preview) { singleton = new Mutex(true, "Local\\LinkLAN.WPF." + Identity, out bool created); if (!created) { Send("show"); singleton.Dispose(); return 0; } }
        var app = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
        app.DispatcherUnhandledException += (_, e) => { Log(e.Exception); if (check) { CheckFailure(args, e.Exception); app.Shutdown(1); } else MessageBox.Show(e.Exception.Message, "LinkLAN", MessageBoxButton.OK, MessageBoxImage.Error); e.Handled = true; };
        try
        {
            Appearance.Load(); var bridgeArgs = BridgeArguments(args, check); var engine = new EngineClient(bridgeArgs); var shell = new MainShell(engine, args, preview); app.MainWindow = shell.Window;
            if (!preview) _ = Listen(shell);
            app.Startup += async (_, _) => { try { await shell.Initialize(); if (check) { int result = await UiChecks.Run(shell, args); await shell.Quit(); Environment.ExitCode = result; } else shell.Start(); } catch (Exception e) { Log(e); if (check) CheckFailure(args, e); else MessageBox.Show(e.Message, "LinkLAN", MessageBoxButton.OK, MessageBoxImage.Error); await shell.Quit(); Environment.ExitCode = 1; } };
            app.Run(); return Environment.ExitCode;
        }
        catch (Exception e) { Log(e); if (check) CheckFailure(args, e); else MessageBox.Show(e.Message, "LinkLAN", MessageBoxButton.OK, MessageBoxImage.Error); return 1; }
        finally { if (singleton != null) { try { singleton.ReleaseMutex(); } catch { } singleton.Dispose(); } }
    }
    static void CheckFailure(string[] args, Exception error) { int i = Array.IndexOf(args, "--validation-dir"); string path = i >= 0 && i + 1 < args.Length ? args[i + 1] : Path.Combine(AppContext.BaseDirectory, "validation"); Directory.CreateDirectory(path); File.WriteAllText(Path.Combine(path, "wpf-check.json"), System.Text.Json.JsonSerializer.Serialize(new { passed = false, error = error.ToString() })); }
    static string[] BridgeArguments(string[] args, bool check) { var result = new List<string>(); for (int i = 0; i < args.Length; i++) { if (args[i].StartsWith("--ui-preview", StringComparison.Ordinal)) { result.Add(args[i]); if (args[i] is "--ui-preview-theme" or "--ui-preview-language" or "--ui-preview-dpi") { if (i + 1 < args.Length) result.Add(args[++i]); } } else if (args[i] == "--allow-http") result.Add(args[i]); } if (check && !result.Contains("--ui-preview")) result.Add("--ui-preview-large"); return result.ToArray(); }
    static int Forward(string[] args) { var start = new ProcessStartInfo(Path.Combine(AppContext.BaseDirectory, "LinkLAN-engine.exe")) { UseShellExecute = false }; foreach (var arg in args) start.ArgumentList.Add(arg); using var child = Process.Start(start); child!.WaitForExit(); return child.ExitCode; }
    static int QuitExisting()
    {
        Send("quit");
        if(!Mutex.TryOpenExisting("Local\\LinkLAN.WPF."+Identity,out var existing))return 0;
        using(existing){bool acquired=false;try{try{acquired=existing.WaitOne(TimeSpan.FromSeconds(70));}catch(AbandonedMutexException){acquired=true;}return acquired?0:1;}finally{if(acquired)existing.ReleaseMutex();}}
    }
    static void Send(string command) { try { using var pipe = new NamedPipeClientStream(".", PipeName, PipeDirection.Out); pipe.Connect(2500); using var writer = new StreamWriter(pipe, new UTF8Encoding(false)) { AutoFlush = true }; writer.WriteLine(command); } catch { } }
    static async Task Listen(MainShell shell) { while (!shell.Exiting) { try { await using var pipe = new NamedPipeServerStream(PipeName, PipeDirection.In, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly); await pipe.WaitForConnectionAsync(shell.Lifetime.Token); using var reader = new StreamReader(pipe); string? command = await reader.ReadLineAsync(shell.Lifetime.Token).AsTask().WaitAsync(TimeSpan.FromSeconds(3)); if (command == "show") await shell.Window.Dispatcher.InvokeAsync(shell.Restore); else if (command == "quit") await shell.Window.Dispatcher.InvokeAsync(async () => await shell.Quit()); } catch (OperationCanceledException) { break; } catch (Exception e) { Log(e); await Task.Delay(500); } } }
    public static void Log(Exception e) { try { string dir = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "LinkLAN"); Directory.CreateDirectory(dir); File.AppendAllText(Path.Combine(dir, "desktop.log"), DateTimeOffset.Now.ToString("O") + " " + e.GetType().Name + ": " + e.Message + Environment.NewLine); } catch { } }
}
