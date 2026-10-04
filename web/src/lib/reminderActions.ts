import { API, type Reminder } from './api'
import { toast } from './toast.svelte'

// 完成待办的口径只有一处：首页和待办页都得给出同样的说法。
// 派生待办（anniv: / promise:）的「完成」写的是来源记录，没有反向接口，所以不给撤销。
export async function completeReminder(r: Reminder, reload: () => Promise<void>) {
  try {
    await API.post(`/api/v1/reminders/${r.id}/done`, {})
    if (r.id.startsWith('promise:')) toast.ok('已记为承诺兑现，如需撤回请到「对话」里改')
    else if (r.id.startsWith('anniv:')) toast.ok('本次提醒已关闭')
    else toast.undoable('已标记完成', async () => {
      // PUT 是全量更新，必须带上原对象的其他字段，否则会被置空
      await API.put(`/api/v1/reminders/${r.id}`, { ...r, status: 'pending', completed_at: '' })
        .catch((err) => toast.fail('撤销失败', err))
      await reload()
    })
    await reload()
  } catch (err) {
    // 失败后重拉一次：界面上已经打过的勾要退回服务器的口径，不能停在「看起来完成了」
    toast.fail('操作失败', err)
    await reload()
  }
}
