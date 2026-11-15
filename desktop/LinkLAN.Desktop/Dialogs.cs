using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Shell;

namespace LinkLAN.Desktop;

public sealed class Dialog
{
    public Window Window { get; }
    public StackPanel Body { get; } = new() { Margin = new Thickness(18, 12, 18, 8) }; public StackPanel Buttons { get; } = new() { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(12) }; public TextBlock Error { get; } = new() { TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 8, 0, 0) };
    public Dialog(MainShell shell, string title, double width = 390, double height = 0)
    {
        Window = new Window { Title = title, Owner = shell.Window, Width = width, MinWidth = Math.Min(width, 320), SizeToContent = height == 0 ? SizeToContent.Height : SizeToContent.Manual, Height = height == 0 ? 400 : height, MaxHeight = Math.Max(350, SystemParameters.WorkArea.Height - 40), ResizeMode = ResizeMode.NoResize, WindowStartupLocation = WindowStartupLocation.CenterOwner, ShowInTaskbar = false, DataContext = shell.VM, Icon = shell.Window.Icon }; WindowChrome.SetWindowChrome(Window, new WindowChrome { CaptionHeight = 34, ResizeBorderThickness = new Thickness(0), CornerRadius = new CornerRadius(8), GlassFrameThickness = new Thickness(0) });
        var grid = new Grid(); grid.RowDefinitions.Add(new() { Height = new GridLength(34) }); grid.RowDefinitions.Add(new() { Height = new GridLength(1, GridUnitType.Star) }); grid.RowDefinitions.Add(new() { Height = GridLength.Auto }); var header = new Grid(); header.SetResourceReference(Panel.BackgroundProperty, "Panel"); var label = new TextBlock { Text = title, Margin = new Thickness(12, 0, 38, 0), FontWeight = FontWeights.SemiBold, TextTrimming = TextTrimming.CharacterEllipsis }; label.SetBinding(TextBlock.TextProperty, new Binding("Title") { Source = Window }); header.Children.Add(label); var close = new Button { Content = "×", HorizontalAlignment = HorizontalAlignment.Right, Style = (Style)Application.Current.FindResource("TitleButton") }; WindowChrome.SetIsHitTestVisibleInChrome(close, true); close.Click += (_, _) => Window.Close(); header.Children.Add(close); grid.Children.Add(header); var scroll = new ScrollViewer { Content = Body, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled }; Grid.SetRow(scroll, 1); grid.Children.Add(scroll); Grid.SetRow(Buttons, 2); grid.Children.Add(Buttons); var border = new Border { CornerRadius = new CornerRadius(8), BorderThickness = new Thickness(1), Child = grid }; border.SetResourceReference(Border.BackgroundProperty, "Background"); border.SetResourceReference(Border.BorderBrushProperty, "InputBorder"); Window.Content = border; Error.SetResourceReference(TextBlock.ForegroundProperty, "Bad"); Window.PreviewKeyDown += (_, e) => { if (e.Key == Key.Escape) { Window.Close(); e.Handled = true; } };
    }
    public Button Button(string label, Action action, bool primary = false) { var button = new Button { Content = label, MinWidth = 85, Margin = new Thickness(6, 0, 0, 0) }; if (primary) button.Style = (Style)Application.Current.FindResource("Primary"); button.Click += (_, _) => action(); Buttons.Children.Add(button); return button; }
    public static void Label(Panel panel, string label) { panel.Children.Add(new TextBlock { Text = label, Margin = new Thickness(0, 10, 0, 5), TextWrapping = TextWrapping.Wrap }); }
    public static TextBox Field(Panel panel, string label, string value = "") { Label(panel, label); var field = new TextBox { Text = value, MinWidth = 100 }; panel.Children.Add(field); return field; }
    public static PasswordBox Password(Panel panel, string label) { Label(panel, label); var field = new PasswordBox(); panel.Children.Add(field); return field; }
    public static void Message(MainShell shell, string title, string text, bool copy = false) { var d = new Dialog(shell, title); d.Body.Children.Add(new TextBox { Text = text, IsReadOnly = true, TextWrapping = TextWrapping.Wrap, BorderThickness = new Thickness(0), Background = Brushes.Transparent, MaxHeight = 400 }); if (copy) d.Button(shell.VM.L["Копировать"], () => shell.Copy(text)); d.Button(shell.VM.L["Закрыть"], () => d.Window.Close(), true); d.Window.ShowDialog(); }
    public static bool Confirm(MainShell shell, string title, string text) { var d = new Dialog(shell, title); d.Body.Children.Add(new TextBlock { Text = text, TextWrapping = TextWrapping.Wrap }); bool yes = false; d.Button(shell.VM.L["Отмена"], () => d.Window.Close()); d.Button(shell.VM.L["Да"], () => { yes = true; d.Window.Close(); }, true); d.Window.ShowDialog(); return yes; }
}
public static class Dialogs
{
    public static Window Settings(MainShell shell, int initial = 0)
    {
        var vm = shell.VM;
        var d = new Dialog(shell, vm.L["Настройки"], 500, 390);
        d.Window.ResizeMode = ResizeMode.CanResize;
        d.Window.MinWidth = 440; d.Window.MinHeight = 360;
        d.Body.Margin = new Thickness(10, 10, 10, 0);
        var split = new Grid { MinHeight = 245 };
        split.ColumnDefinitions.Add(new() { Width = new GridLength(150) });
        split.ColumnDefinitions.Add(new() { Width = new GridLength(1, GridUnitType.Star) });
        var navigation = new ListBox { BorderThickness = new Thickness(0), Background = Brushes.Transparent, ItemContainerStyle = (Style)Application.Current.FindResource("Nav"), Padding = new Thickness(0, 5, 0, 5) };
        navigation.SetResourceReference(Control.ForegroundProperty, "Text");
        var sidebar = new Border { CornerRadius = new CornerRadius(7), Child = navigation };
        sidebar.SetResourceReference(Border.BackgroundProperty, "Panel");
        var page = new StackPanel { Margin = new Thickness(16, 0, 2, 0) };
        Grid.SetColumn(page, 1); split.Children.Add(sidebar); split.Children.Add(page);
        d.Body.Children.Add(split); d.Body.Children.Add(d.Error);
        var close = d.Button(vm.L["Закрыть"], () => d.Window.Close(), true);
        close.SetBinding(ContentControl.ContentProperty, new Binding("L[Закрыть]") { Source = vm });
        string[] labels = ["Тема", "Язык", "Дополнительно", "Уведомления", "Сервер", "Админ-панель"];
        string[] icons = ["M8,1 A7,7 0 1 0 8,15 L10,15 A2,2 0 0 0 10,11 L9,11 A1,1 0 0 1 9,9 L13,9 A2,2 0 0 0 15,7 A7,7 0 0 0 8,1 M4,5 L5,5 M8,3 L9,3 M11,5 L12,5", "M1,3 L10,3 M5,1 L5,3 M3,3 Q3,9 10,11 M8,3 Q8,9 1,11 M9,15 L12,7 L15,15 M10,12 L14,12", "M2,2 L14,2 L14,6 L2,6 Z M2,10 L14,10 L14,14 L2,14 Z M5,4 L6,4 M5,12 L6,12", "M3,11 L3,6 A5,5 0 0 1 13,6 L13,11 L15,13 L1,13 Z M6,15 L10,15", "M2,1 L14,1 L14,15 L2,15 Z M4,4 L5,4 M7,4 L12,4 M4,8 L5,8 M7,8 L12,8 M4,12 L5,12 M7,12 L12,12", "M8,1 L14,3 L14,8 Q14,12 8,15 Q2,12 2,8 L2,3 Z M5,8 L7,10 L11,5"];
        async Task<bool> Patch(object patch)
        {
            try { await shell.Patch(patch); d.Error.Text = ""; return true; }
            catch (Exception e) { d.Error.Text = e.Message; return false; }
        }
        void Toggle(string label, bool value, Func<bool, object> patch)
        {
            var c = new CheckBox { Content = new TextBlock { Text = vm.L[label], TextWrapping = TextWrapping.Wrap, FontSize = 12 }, IsChecked = value, Style = (Style)Application.Current.FindResource("Switch") };
            c.Click += async (_, _) => { c.IsEnabled = false; try { if (!await Patch(patch(c.IsChecked == true))) c.IsChecked = value;else value=c.IsChecked==true; } finally { c.IsEnabled = true; } };
            page.Children.Add(c);
        }
        void Radio(string label, bool chosen, object patch)
        {
            var r = new RadioButton { Content = vm.L[label], IsChecked = chosen, GroupName = "choices" };
            r.Click += async (_, _) => { if (!await Patch(patch)) { string error = d.Error.Text; Render(); d.Error.Text = error; } };
            page.Children.Add(r);
        }
        void Render()
        {
            page.Children.Clear(); d.Error.Text = ""; var p = vm.Settings;
            switch (navigation.SelectedIndex)
            {
                case 0:
                    Dialog.Label(page, vm.L["Выберите цветовую схему"]);
                    Radio("Светлая", p.Theme == "light", new { theme = "light" });
                    Radio("Тёмная", p.Theme == "dark", new { theme = "dark" });
                    Radio("Автоматически", p.Theme == "auto", new { theme = "auto" });
                    break;
                case 1:
                    Dialog.Label(page, vm.L["Выберите язык"]);
                    Radio("Русский", p.Language == "ru", new { language = "ru" });
                    Radio("English", p.Language == "en", new { language = "en" });
                    break;
                case 2:
                    Toggle("Подключаться автоматически", p.AutoConnect, v => new { autoConnect = v });
                    Toggle("Запускать с Windows", p.StartAtLogon, v => new { startAtLogon = v });
                    Toggle("Запускать свёрнутым в трей", p.StartInTray, v => new { startInTray = v });
                    break;
                case 3:
                    Toggle("Показывать уведомления", p.NotifyDesktop, v => new { notifyDesktop = v });
                    Toggle("Звук уведомлений", p.NotifySound, v => new { notifySound = v });
                    Toggle("Учитывать режим «Не беспокоить»", p.RespectFocus, v => new { respectFocus = v });
                    break;
                case 4:
                    var server = Dialog.Field(page, vm.L["Адрес API-сервера"], p.Server);
                    var name = Dialog.Field(page, vm.L["Имя компьютера"], p.DeviceName);
                    var button = new Button { Content = vm.L["Сохранить"], Margin = new Thickness(0, 15, 0, 0), HorizontalAlignment = HorizontalAlignment.Left, Style = (Style)Application.Current.FindResource("Primary") };
                    button.Click += async (_, _) => { button.IsEnabled = false; d.Error.Text = vm.L["Подключение…"]; try { await shell.Engine.Call("server", new { server = server.Text.Trim(), deviceName = name.Text.Trim() }); await shell.Refresh(); d.Error.Text = ""; } catch (Exception e) { d.Error.Text = e.Message; } finally { button.IsEnabled = true; } };
                    page.Children.Add(button); break;
                case 5:
                    Dialog.Label(page, vm.L["Админ-панель"]);
                    page.Children.Add(new TextBlock { Text = vm.L["Панель откроется в браузере. Введите токен администратора на странице входа."], TextWrapping = TextWrapping.Wrap, FontSize = 12, Margin = new Thickness(0, 0, 0, 12) });
                    var open = new Button { Content = vm.L["Открыть"], HorizontalAlignment = HorizontalAlignment.Left };
                    open.Click += (_, _) => { if (Uri.TryCreate(p.Server, UriKind.Absolute, out var uri) && uri.Scheme is "https" or "http") shell.Open(MainShell.AdminAddress(uri)); else d.Error.Text = vm.L["Укажите сервер в настройках"]; };
                    page.Children.Add(open); break;
            }
        }
        void Culture()
        {
            int selected = navigation.SelectedIndex;
            navigation.Items.Clear();
            for (int i = 0; i < labels.Length; i++)
            {
                var row = new Grid(); row.ColumnDefinitions.Add(new() { Width = new GridLength(28) }); row.ColumnDefinitions.Add(new() { Width = new GridLength(1, GridUnitType.Star) });
                var icon = new System.Windows.Shapes.Path { Data = Geometry.Parse(icons[i]), StrokeThickness = 1.3, StrokeLineJoin = PenLineJoin.Round, StrokeStartLineCap = PenLineCap.Round, StrokeEndLineCap = PenLineCap.Round, Width = 19, Height = 19, Stretch = Stretch.Uniform, HorizontalAlignment = HorizontalAlignment.Left };
                icon.SetResourceReference(System.Windows.Shapes.Shape.StrokeProperty, "Name"); row.Children.Add(icon);
                var label = new TextBlock { Text = vm.L[labels[i]], FontSize = 12, TextWrapping = TextWrapping.Wrap }; Grid.SetColumn(label, 1); row.Children.Add(label); navigation.Items.Add(row);
            }
            navigation.SelectedIndex = Math.Max(0, selected); d.Window.Title = vm.L["Настройки"]; Render();
        }
        navigation.SelectionChanged += (_, _) => Render(); Culture(); navigation.SelectedIndex = Math.Clamp(initial, 0, 5);
        vm.CultureChanged += Culture; d.Window.Closed += (_, _) => vm.CultureChanged -= Culture;
        return d.Window;
    }
    public static void Network(MainShell shell, bool create) { var vm = shell.VM; var d = new Dialog(shell, vm.L[create ? "Создать сеть" : "Присоединиться к сети…"]); var name = Dialog.Field(d.Body, vm.L["Имя сети"]); var mode = new CheckBox { Content = vm.L[create ? "По приглашению" : "Использовать код приглашения"], Style = (Style)Application.Current.FindResource("Switch") }; d.Body.Children.Add(mode); var pass = Dialog.Password(d.Body, vm.L["Пароль"]); PasswordBox? confirmation = create ? Dialog.Password(d.Body, vm.L["Подтверждение пароля"]) : null; TextBox? invite = !create ? Dialog.Field(d.Body, vm.L["Код приглашения"]) : null; if (invite != null) invite.IsEnabled = false; mode.Click += (_, _) => { pass.IsEnabled = mode.IsChecked != true; if (confirmation != null) confirmation.IsEnabled = pass.IsEnabled; if (invite != null) { invite.IsEnabled = mode.IsChecked == true; name.IsEnabled = !invite.IsEnabled; } }; d.Body.Children.Add(d.Error); d.Button(vm.L["Отмена"], () => d.Window.Close()); Button? submit = null; submit = d.Button(vm.L[create ? "Создать" : "Присоединиться"], async () => { submit!.IsEnabled = false; try { await shell.Engine.Call(create ? "create" : "join", new { name = name.Text.Trim(), password = pass.Password, confirm = confirmation?.Password ?? pass.Password, mode = mode.IsChecked == true ? "invite" : "password", invite = mode.IsChecked == true ? invite?.Text.Trim() : "" }); pass.Clear(); confirmation?.Clear(); await shell.Refresh(); d.Window.Close(); } catch (Exception e) { d.Error.Text = e.Message; } finally { submit.IsEnabled = true; } }, true); submit.IsDefault = true; d.Window.ShowDialog(); }
    public static void Properties(MainShell shell, NodeVM node) => PropertiesWindow(shell, node).ShowDialog();
    public static Window PropertiesWindow(MainShell shell, NodeVM node)
    {
        var l = shell.VM.L; var d = new Dialog(shell, l["Свойства"] + ": " + node.Name, 370);
        d.Body.Margin = new Thickness(10, 7, 10, 0);
        Grid Section(string title) { var rows = new Grid(); var box = new GroupBox { Header = l[title], Content = rows }; rows.ColumnDefinitions.Add(new() { Width = new GridLength(98) }); rows.ColumnDefinitions.Add(new() { Width = new GridLength(1, GridUnitType.Star) }); d.Body.Children.Add(box); return rows; }
        void Row(Grid rows, string label, string value) { int i = rows.RowDefinitions.Count; rows.RowDefinitions.Add(new() { Height = GridLength.Auto }); var key = new TextBlock { Text = l[label] + ":", FontSize = 12, TextWrapping=TextWrapping.Wrap, Margin = new Thickness(0, 3, 6, 3) }; Grid.SetRow(key, i); rows.Children.Add(key); var text = new TextBox { Text = string.IsNullOrEmpty(value) ? "—" : value, IsReadOnly = true, IsReadOnlyCaretVisible = false, MinHeight = 0, FontSize = 12, Padding = new Thickness(0, 3, 0, 3), BorderThickness = new Thickness(0), Background = Brushes.Transparent }; Grid.SetRow(text, i); Grid.SetColumn(text, 1); rows.Children.Add(text); }
        var device = Section("Свойства"); Row(device, "Имя компьютера", node.Name); Row(device, "ID устройства", node.Id); Row(device, "IP-адрес", node.Ip); Row(device, "Отпечаток ключа", node.Peer?.Fingerprint ?? "");
        var network = Section("Состояние сети"); Row(network, "Имя сети", node.Network.Name); Row(network, "Роль", l[node.Network.OwnerId == node.Peer?.UserId ? "Администратор" : "Участник"]);
        var connection = Section("Статус подключения"); Row(connection, "Статус", l[node.Online ? "В сети" : "Не в сети"]); Row(connection, "Тип соединения", node.Peer?.Route ?? ""); Row(connection, "Задержка", node.Peer is { Measured: true, Reachable: true, RttMs: long ms } ? (ms < 1 ? "<1" : ms.ToString()) + " ms" : "—");
        d.Button(l["Закрыть"], () => d.Window.Close(), true); return d.Window;
    }
}
