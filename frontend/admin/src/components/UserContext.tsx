// 全局登录用户上下文：登录成功后注入，供需要当前用户信息的页面读取。
import { createContext, useContext } from 'react'
import type { User } from '../api/types'

const UserContext = createContext<User | null>(null)

export function UserProvider({ user, children }: { user: User | null; children: React.ReactNode }) {
  return <UserContext.Provider value={user}>{children}</UserContext.Provider>
}

/** 读取当前登录用户；未登录时返回 null。 */
export function useUser() {
  return useContext(UserContext)
}
