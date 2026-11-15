using System.Collections.Concurrent;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Text.Json;

namespace LinkLAN.Desktop;

public sealed class EngineClient : IAsyncDisposable
{
    public static readonly JsonSerializerOptions Json = new() { PropertyNamingPolicy = JsonNamingPolicy.CamelCase, PropertyNameCaseInsensitive = true };
    readonly Process process; readonly SemaphoreSlim writer = new(1, 1); readonly ConcurrentDictionary<long, TaskCompletionSource<JsonElement>> pending = []; long sequence; bool closed;
    public int ProcessId => process.Id;
    public EngineClient(string[] args) { var start = new ProcessStartInfo(Path.Combine(AppContext.BaseDirectory, "LinkLAN-engine.exe")) { UseShellExecute = false, CreateNoWindow = true, RedirectStandardInput = true, RedirectStandardOutput = true, RedirectStandardError = true, StandardOutputEncoding = Encoding.UTF8, StandardErrorEncoding = Encoding.UTF8 }; start.ArgumentList.Add("--ui-bridge"); foreach (var arg in args) start.ArgumentList.Add(arg); process = Process.Start(start) ?? throw new InvalidOperationException("Could not start the LinkLAN engine"); _ = Read(); _ = DrainErrors(); }
    async Task DrainErrors() { while (await process.StandardError.ReadLineAsync() != null) { } }
    async Task Read() { try { while (await process.StandardOutput.ReadLineAsync() is string line) { using var document = JsonDocument.Parse(line); var frame = document.RootElement; long id = frame.GetProperty("id").GetInt64(); if (!pending.TryRemove(id, out var completion)) continue; if (frame.TryGetProperty("error", out var error)) completion.TrySetException(new InvalidOperationException(error.GetString())); else completion.TrySetResult(frame.TryGetProperty("result", out var value) ? value.Clone() : default); } } catch (Exception e) { foreach (var p in pending.Values) p.TrySetException(e); } finally { foreach (var p in pending.Values) p.TrySetException(new IOException("The LinkLAN engine stopped")); pending.Clear(); } }
    public async Task<JsonElement> Call(string method, object? parameters = null) { if (closed) throw new ObjectDisposedException(nameof(EngineClient)); long id = Interlocked.Increment(ref sequence); var done = new TaskCompletionSource<JsonElement>(TaskCreationOptions.RunContinuationsAsynchronously); pending[id] = done; try { await writer.WaitAsync(); try { await process.StandardInput.WriteLineAsync(JsonSerializer.Serialize(new { id, method, @params = parameters }, Json)); await process.StandardInput.FlushAsync(); } finally { writer.Release(); } return await done.Task.WaitAsync(TimeSpan.FromSeconds(95)); } finally { pending.TryRemove(id, out _); } }
    public async Task<PublicState> State() => (await Call("state")).Deserialize<PublicState>(Json) ?? throw new IOException("Invalid engine state");
    public async ValueTask DisposeAsync() { if (closed) return; try { await Call("shutdown").WaitAsync(TimeSpan.FromSeconds(3)); } catch { } closed = true; try { process.StandardInput.Close(); await process.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(22)); } catch { try { process.Kill(true); } catch { } } process.Dispose(); writer.Dispose(); }
}
public static class Resource
{
    public static Stream Open(string suffix) { var assembly = typeof(Resource).Assembly; var name = assembly.GetManifestResourceNames().Single(n => n.EndsWith(suffix, StringComparison.Ordinal)); return assembly.GetManifestResourceStream(name)!; }
    public static string Text(string suffix) { using var stream = Open(suffix); using var reader = new StreamReader(stream); return reader.ReadToEnd(); }
}
