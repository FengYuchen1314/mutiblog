// 顶栏活动指示器：活跃渲染/翻译任务数与断线降级提示。
import { useActivity } from '../hooks/useActivity'

export default function ActivityIndicator() {
  const { activity, degraded } = useActivity()
  const active = activity.jobs.running + activity.jobs.pending + activity.translations.length
  return (
    <div className="activity-indicator">
      <span className={active > 0 ? 'busy' : ''}>{active > 0 ? '活跃 ' + active : '空闲'}</span>
      <small>
        渲染 {activity.jobs.running} · 翻译 {activity.translations.length}
      </small>
      {degraded && <span className="degraded">实时推送中断，已降级为 5 秒轮询</span>}
    </div>
  )
}
