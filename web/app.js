'use strict';
const demo=document.querySelector('.network-demo'),p2p=document.getElementById('p2p'),relay=document.getElementById('relay'),note=document.getElementById('route-note');
function choose(useRelay){demo.classList.toggle('via-relay',useRelay);p2p.setAttribute('aria-pressed',String(!useRelay));relay.setAttribute('aria-pressed',String(useRelay));note.textContent=useRelay?'Пример сети: relay пересылает пакеты, сохраняя E2E-шифрование.':'Пример сети: компьютеры обмениваются пакетами напрямую.'}
p2p.addEventListener('click',()=>choose(false));relay.addEventListener('click',()=>choose(true));
