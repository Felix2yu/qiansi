// 六类主记录的删除不再直接消失：DELETE 只把它们送进回收站。
// 这里集中两件事——就地撤销（5 秒）与回收站页的恢复/彻底删除，
// 免得每个页面各写一遍「删了又要恢复」的分支。
import { API } from './api'
import { toast } from './toast.svelte'

export type TrashKind = 'person' | 'event' | 'memo' | 'transaction' | 'anniversary' | 'reminder'

export const TRASH_LABEL: Record<TrashKind, string> = {
  person: '联系人',
  event: '往来',
  memo: '对话',
  transaction: '账目',
  anniversary: '纪念日',
  reminder: '待办',
}

// 删除仍走各自的资源路径（只置 deleted_at），恢复与彻底删除走回收站路径。
const ENDPOINT: Record<TrashKind, string> = {
  person: '/api/v1/people',
  event: '/api/v1/events',
  memo: '/api/v1/memos',
  transaction: '/api/v1/transactions',
  anniversary: '/api/v1/anniversaries',
  reminder: '/api/v1/reminders',
}

export type TrashItem = {
  type: TrashKind
  id: string
  title: string
  detail?: string
  person_name?: string
  deleted_at: string
  /** 仅联系人：恢复这个人会一并带回多少条记录 */
  hidden?: number
  /** 归属的联系人还在回收站：这条即使恢复也暂时看不见 */
  person_trashed: boolean
}

/** 删除确认框统一的那句提示：写清楚去处，人才敢点 */
export const RECOVER_NOTE = '删除后可在回收站找回。'

export async function restoreTrash(kind: TrashKind, id: string): Promise<void> {
  await API.post(`/api/v1/trash/${kind}/${id}/restore`, {})
}

/**
 * 删一条记录并在原地点给一次反悔机会：撤销走回收站恢复，不重新创建，
 * 所以 id、时间戳、关联关系都原样。返回是否真的删掉了，调用方据此收弹窗。
 */
export async function trashOne(
  kind: TrashKind,
  id: string,
  reload: () => Promise<unknown> | void,
  text = '已删除',
): Promise<boolean> {
  try {
    await API.delete(`${ENDPOINT[kind]}/${id}`)
  } catch (err) {
    toast.fail('删除失败', err)
    return false
  }
  toast.undoable(text, async () => {
    try {
      await restoreTrash(kind, id)
      toast.ok('已恢复')
    } catch (err) {
      toast.fail('恢复失败', err)
    }
    await reload()
  })
  await reload()
  return true
}
