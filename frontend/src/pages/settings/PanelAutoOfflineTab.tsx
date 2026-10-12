import { useEffect, useMemo, useState } from 'react';
import { Input, Select, Switch } from 'antd';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';

type Unit = 'minute' | 'hour' | 'day';
const MAX:Record<Unit,number>={minute:525600,hour:8760,day:365};
export default function PanelAutoOfflineTab({allSetting,updateSetting,onValidityChange}:{
 allSetting:AllSetting;updateSetting:(v:Partial<AllSetting>)=>void;
 onValidityChange?:(valid:boolean)=>void;
}){
 const [text,setText]=useState(()=>String(allSetting.panelOfflineValue));
 useEffect(()=>setText(String(allSetting.panelOfflineValue)),[allSetting.panelOfflineValue]);
 const valid=useMemo(()=>/^[1-9]\d*$/.test(text)&&Number.isSafeInteger(Number(text))&&Number(text)<=MAX[allSetting.panelOfflineUnit],
  [text,allSetting.panelOfflineUnit]);
 useEffect(()=>onValidityChange?.(!allSetting.panelOfflineEnabled||valid),
  [allSetting.panelOfflineEnabled,valid,onValidityChange]);
 return <>
  <SettingListItem title="自动离线" description="每次面板启动后开始计时，到期只停止面板进程并保留代理内核。">
   <Switch checked={allSetting.panelOfflineEnabled} onChange={v=>updateSetting({panelOfflineEnabled:v})}/>
  </SettingListItem>
  <SettingListItem title="离线倒计时" description="必须是大于零的整数，最多365天；错误值立即标红。">
   <div style={{display:'flex',gap:8,width:'100%'}}>
    <Input value={text} disabled={!allSetting.panelOfflineEnabled}
     status={allSetting.panelOfflineEnabled&&!valid?'error':undefined}
     aria-invalid={allSetting.panelOfflineEnabled&&!valid}
     onChange={e=>{
       const raw=e.target.value;setText(raw);
       if(/^[1-9]\d*$/.test(raw)&&Number.isSafeInteger(Number(raw))&&Number(raw)<=MAX[allSetting.panelOfflineUnit]){
         updateSetting({panelOfflineValue:Number(raw)});
       }
     }}/>
    <Select style={{minWidth:110}} value={allSetting.panelOfflineUnit}
     options={[{label:'分钟',value:'minute'},{label:'小时',value:'hour'},{label:'天',value:'day'}]}
     onChange={(v:Unit)=>updateSetting({panelOfflineUnit:v})}/>
   </div>
  </SettingListItem>
 </>;
}
