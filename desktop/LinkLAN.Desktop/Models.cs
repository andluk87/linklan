using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Runtime.CompilerServices;
using System.Text.Json;
using System.Windows;
using System.Windows.Input;

namespace LinkLAN.Desktop;
public class WindowPosition { public int X{get;set;} public int Y{get;set;} public int Width{get;set;} public int Height{get;set;} public bool Positioned{get;set;} }
public class Preferences {
 public string Server{get;set;}="";public string DeviceName{get;set;}="";public string Theme{get;set;}="light";public string Language{get;set;}="ru";
 public bool AutoConnect{get;set;} public bool StartAtLogon{get;set;}=true;public bool StartInTray{get;set;}=true;public bool NotifyDesktop{get;set;}=true;public bool NotifySound{get;set;}=true;public bool RespectFocus{get;set;}=true;public bool FirewallAllowed{get;set;}
 public WindowPosition Window{get;set;}=new(); public bool WindowDip{get;set;} public Dictionary<string,bool>? Expanded{get;set;}
}
public class PeerInfo {public string Id{get;set;}="";public string UserId{get;set;}="";public string Name{get;set;}="";public string Ip{get;set;}="";public bool Online{get;set;} public string Status{get;set;}="";public string Route{get;set;}="";public bool Measured{get;set;} public bool Reachable{get;set;} public long? RttMs{get;set;} public string Fingerprint{get;set;}="";}
public class NetworkInfo {public string Id{get;set;}="";public string Name{get;set;}="";public string OwnerId{get;set;}="";public string Description{get;set;}="";public string Subnet{get;set;}="";public string Ip{get;set;}="";public string Mode{get;set;}="";public string Status{get;set;}="";public bool Blocked{get;set;} public int MaxMembers{get;set;} public List<PeerInfo> Peers{get;set;}=[];}
public class PublicState {public string Version{get;set;}="";public ulong Revision{get;set;} public Preferences Settings{get;set;}=new(); public bool ApiReady{get;set;}public bool Busy{get;set;} public bool Connected{get;set;} public bool Wanted{get;set;} public bool Reconnecting{get;set;} public string Error{get;set;}="";public string Message{get;set;}="";public string ActiveNetworkId{get;set;}="";public string UserId{get;set;}="";public string DeviceId{get;set;}="";public string OwnIp{get;set;}="";public List<NetworkInfo> Networks{get;set;}=[];}
public class Observable : INotifyPropertyChanged {
 public event PropertyChangedEventHandler? PropertyChanged;
 public void Changed([CallerMemberName]string? name=null)=>PropertyChanged?.Invoke(this,new(name));
 protected bool Set<T>(ref T field,T value,[CallerMemberName]string? name=null){if(EqualityComparer<T>.Default.Equals(field,value))return false;field=value;Changed(name);return true;}
}
public sealed class ActionCommand(Action<object?> action,Func<object?,bool>? enabled=null):ICommand{
 public bool CanExecute(object? p)=>enabled?.Invoke(p)??true;public void Execute(object? p)=>action(p);
 public event EventHandler? CanExecuteChanged{add=>CommandManager.RequerySuggested+=value;remove=>CommandManager.RequerySuggested-=value;}
}
public sealed class Locale : Observable {
 readonly Dictionary<string,string> english;
 public string Language{get;private set;}="ru";
 public Locale(){using var s=Resource.Open("Strings.json");english=JsonSerializer.Deserialize<Dictionary<string,string>>(s)??[];}
 public string this[string ru]=>Language=="en"&&english.TryGetValue(ru,out var en)?en:ru;
 public void Apply(string language){if(Language==language)return;Language=language;Changed("Item[]");}
}
public sealed class NodeVM : Observable {
 public MainVM Owner{get;} public bool IsNetwork{get;} public string NetworkId{get;} public string Id{get;}
 public string Key=>NetworkId+"/"+Id;
 public ObservableCollection<NodeVM> Children{get;}=[];
 public NetworkInfo Network{get;private set;} public PeerInfo? Peer{get;private set;}
 bool expanded=true,selected;
 public bool Expanded{get=>expanded;set{if(Set(ref expanded,value))Owner.ExpandedChanged(NetworkId,value);}}
 public bool Selected{get=>selected;set=>Set(ref selected,value);}
 public string Name=>IsNetwork?Network.Name:Peer?.Name??"";
 public string Ip=>Peer?.Ip??"";
 public bool Online=>Peer?.Online??false;
 public string OfflineMark=>Online?"":"×";
 public int Signal=>Peer is {Online:true,Measured:true,Reachable:true,RttMs:long ms}?ms<=50?4:ms<=100?3:ms<=200?2:1:0;
 public string Tip=>IsNetwork?Network.Description:Name+" · "+(Online?Peer?.Measured==true?Peer.Reachable?((Peer.RttMs??0)<1?"<1":Peer.RttMs.ToString())+" ms":Owner.L["Нет ответа ICMP; соединение не подтверждено"]:Owner.L["В сети; проверка ICMP ещё не выполнена"]:Owner.L["Компьютер не в сети"]);
 public NodeVM(MainVM owner,NetworkInfo network,PeerInfo? peer=null){Owner=owner;Network=network;Peer=peer;IsNetwork=peer==null;NetworkId=network.Id;Id=peer?.Id??network.Id;if(IsNetwork)expanded=owner.Expanded.TryGetValue(network.Id,out var open)?open:true;}
 public void Update(NetworkInfo network,PeerInfo? peer=null){var oldName=Name;var oldIp=Ip;var online=Online;var signal=Signal;var tip=Tip;Network=network;Peer=peer;if(Name!=oldName)Changed(nameof(Name));if(Ip!=oldIp)Changed(nameof(Ip));if(Online!=online){Changed(nameof(Online));Changed(nameof(OfflineMark));}if(Signal!=signal)Changed(nameof(Signal));if(Tip!=tip)Changed(nameof(Tip));}
}
public sealed class MainVM : Observable {
 public Locale L{get;}=new(); public ObservableCollection<NodeVM> Networks{get;}=[];public Dictionary<string,bool> Expanded{get;}=[];
 public PublicState State{get;private set;}=new();public Preferences Settings=>State.Settings;
 public string ComputerName=>Settings.DeviceName;public string OwnIp=>string.IsNullOrEmpty(State.OwnIp)?"—":State.OwnIp;
 public bool Connected=>State.Connected;public bool Waiting=>!Connected&&(State.Wanted||State.Busy&&!State.ApiReady);
 public string PowerText=>Connected?"ON":Waiting?"…":"OFF";
 public string Badge=>L[Connected?"В сети":!string.IsNullOrEmpty(State.Error)?"Ошибка подключения":Waiting?State.Reconnecting?"Переподключение…":"Подключение…":"Не в сети"];
 public string EmptyMessage=>State.Busy?L["Подключение…"]:L["Создайте сеть или присоединитесь к существующей."];
 public bool Empty=>Networks.Count==0;
 public NodeVM? SelectedNode{get;set;}
 public event Action<string,bool>? ExpansionChanged;
 public event Action? CultureChanged;
 public MainVM(){ }
 public void RefreshAppearance(){foreach(var root in Networks){root.Changed(nameof(NodeVM.Tip));foreach(var peer in root.Children){peer.Changed(nameof(NodeVM.Signal));peer.Changed(nameof(NodeVM.Tip));}}}
 public void ExpandedChanged(string id,bool value){Expanded[id]=value;ExpansionChanged?.Invoke(id,value);}
 public void Apply(PublicState state){if(state.Revision<State.Revision)return;bool initial=State.Version=="";string previousLanguage=L.Language;State=state;if(initial&&state.Settings.Expanded!=null){foreach(var p in state.Settings.Expanded)Expanded[p.Key]=p.Value;}L.Apply(state.Settings.Language);
  var keep=state.Networks.Select(n=>n.Id).ToHashSet();for(int i=Networks.Count-1;i>=0;i--)if(!keep.Contains(Networks[i].Id))Networks.RemoveAt(i);
  for(int i=0;i<state.Networks.Count;i++){var n=state.Networks[i];var node=Networks.FirstOrDefault(v=>v.Id==n.Id);if(node==null){node=new(this,n);Networks.Insert(i,node);}else{node.Update(n);var index=Networks.IndexOf(node);if(index!=i)Networks.Move(index,i);}
   var peers=n.Peers.Select(p=>p.Id).ToHashSet();for(int j=node.Children.Count-1;j>=0;j--)if(!peers.Contains(node.Children[j].Id))node.Children.RemoveAt(j);
   for(int j=0;j<n.Peers.Count;j++){var p=n.Peers[j];var child=node.Children.FirstOrDefault(v=>v.Id==p.Id);if(child==null){child=new(this,n,p);node.Children.Insert(j,child);}else{child.Update(n,p);int index=node.Children.IndexOf(child);if(index!=j)node.Children.Move(index,j);}}
  }
  foreach(var name in new[]{nameof(Settings),nameof(ComputerName),nameof(OwnIp),nameof(Connected),nameof(Waiting),nameof(PowerText),nameof(Badge),nameof(EmptyMessage),nameof(Empty),nameof(State)})Changed(name);
  if(previousLanguage!=L.Language)CultureChanged?.Invoke();CommandManager.InvalidateRequerySuggested();
 }
}
