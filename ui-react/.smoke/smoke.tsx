import { createRoot } from 'react-dom/client'
import { GuiProvider } from '@hanzo/gui'
import guiConfig from '@hanzo/ui/gui-config'
import { Input, Button } from '@hanzo/ui'

function App() {
  const log = (k: string) => () => { (window as any).__hits = [...((window as any).__hits ?? []), k] }
  return (
    <GuiProvider config={guiConfig} defaultTheme="dark">
      <div id="probe">
        <Input id="sec" secureTextEntry defaultValue="hunter2" />
        <Input id="pwtype" type="password" defaultValue="hunter2" />
        <Button id="b-click" onPress={log('onPress')}>ClickMe</Button>
        <Button id="b-onclick" onClick={log('onClick')}>OnClickMe</Button>
        <form id="f" onSubmit={(e) => { e.preventDefault(); log('formSubmit')() }}>
          <Button type="submit">SubmitMe</Button>
          <button type="submit" id="nativebtn">NativeSubmit</button>
        </form>
      </div>
    </GuiProvider>
  )
}
createRoot(document.getElementById('root')!).render(<App />)
