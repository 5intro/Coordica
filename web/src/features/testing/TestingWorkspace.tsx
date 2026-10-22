import { useRouteField } from '../../hooks/useRouteField';
import { Testing as SuiteTesting } from '../Testing';
import type { TestingProps } from '../Testing';
import { TestCases } from './TestCases';
import { RequirementAcceptance } from './RequirementAcceptance';
import './catalog.css';

export function Testing(props: TestingProps) {
  const [run] = useRouteField('testing', 'run');
  const [tab, setTab] = useRouteField('testing', 'section', 'cases');
  const current = run ? 'execution' : tab;
  if (!props.snapshot.project) return <SuiteTesting {...props} />;
  return <div className="testing-center test-workspace">
    <header className="testing-heading"><h1>测试中心</h1></header>
    <nav className="segmented" aria-label="测试中心视图">
      {([['cases', '测试条目'], ['acceptance', '需求验收与补测'], ['execution', '套件与执行']] as const).map(([id, label]) =>
        <button key={id} aria-pressed={current === id} onClick={() => setTab(id, id === 'execution' ? [] : ['run'])}>{label}</button>)}
    </nav>
    <div hidden={current !== 'cases'}><TestCases {...props} /></div>
    <div hidden={current !== 'acceptance'}><RequirementAcceptance {...props} /></div>
    <div className="test-legacy" hidden={current !== 'execution'}><SuiteTesting {...props} /></div>
  </div>;
}
