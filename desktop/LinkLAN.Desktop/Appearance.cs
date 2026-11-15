using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Markup;
using System.Windows.Media;
using Microsoft.Win32;

namespace LinkLAN.Desktop;
public sealed class InverseVisibilityConverter:IValueConverter {public object Convert(object value,Type targetType,object parameter,CultureInfo culture)=>value is true?Visibility.Collapsed:Visibility.Visible;public object ConvertBack(object value,Type targetType,object parameter,CultureInfo culture)=>throw new NotSupportedException();}
public sealed class SignalColorConverter:IValueConverter {
 public object Convert(object value,Type targetType,object parameter,CultureInfo culture){int level=value is int n?n:0;int bar=int.TryParse(parameter?.ToString(),out var i)?i:1;return Application.Current.Resources[level>=bar?level==1?"Bad":level==2?"Warning":"Good":"Neutral"];}
 public object ConvertBack(object value,Type targetType,object parameter,CultureInfo culture)=>throw new NotSupportedException();
}
public static class Appearance {
 static bool? light;
 public static bool IsLight=>light??true;
 public static void Load(){var resources=(ResourceDictionary)XamlReader.Parse(Resource.Text("Theme.xaml"));Application.Current.Resources.MergedDictionaries.Add(resources);Apply("light",true);}
 public static bool Apply(string mode,bool force=false){bool current=mode!="dark";if(mode=="auto"){try{using var key=Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");current=key?.GetValue("AppsUseLightTheme") is not int v||v!=0;}catch{current=true;}}
  if(light==current&&!force)return false;light=current;
  var colors=current?new Dictionary<string,string>{{"Background","#D7E8F2"},{"Panel","#FFFFFF"},{"Input","#FFFFFF"},{"InputBorder","#B8D5E5"},{"Text","#243F50"},{"Muted","#627786"},{"Accent","#43CBD5"},{"Name","#36A3B5"},{"Selection","#B8DDF6"},{"Inactive","#D5D5D5"},{"Hover","#D6EBF8"},{"Menu","#FAFAFA"},{"Neutral","#C3C9CE"},{"Badge","#348FA4"}}:new Dictionary<string,string>{{"Background","#0E1621"},{"Panel","#252E3D"},{"Input","#192633"},{"InputBorder","#3B5268"},{"Text","#FFFFFF"},{"Muted","#9AAFC0"},{"Accent","#29B7CE"},{"Name","#29B7CE"},{"Selection","#2B5278"},{"Inactive","#465363"},{"Hover","#2F3F4E"},{"Menu","#293C4D"},{"Neutral","#5B6A78"},{"Badge","#348FA4"}};
  colors["Good"]="#23C52B";colors["Warning"]="#E5BD39";colors["Bad"]="#E6694C";
  foreach(var entry in colors){var brush=new SolidColorBrush((Color)ColorConverter.ConvertFromString(entry.Value));brush.Freeze();Application.Current.Resources[entry.Key]=brush;}return true;
 }
}
