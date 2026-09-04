import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { apiClient, setAuthStoreGetter } from '../client';
import { REFETCH_INTERVAL_DEFAULT } from '../constants';
import { queryClientInstance } from '@/lib/query-client-instance';
import { logger } from '@/lib/logger';

/**
 * 用户登录请求
 */
export interface UserLoginRequest {
    username: string;
    password: string;
    totp_code?: string; // 仅当用户启用了 2FA 时需要
    expire: number; // token 过期时间（分钟）
}

/**
 * 注册请求：与后端 register handler 的匿名 struct 对齐。
 * 登录不接受 email/invite_code，故单列一个类型而非复用 UserLoginRequest。
 */
export interface UserRegisterRequest {
    username: string;
    password: string;
    expire: number;
    /** register_invite_required 开启时必填 */
    invite_code?: string;
    /** register_email_required 开启时必填 */
    email?: string;
    email_code?: string;
}

/**
 * 用户登录响应
 */
export interface UserLoginResponse {
    token: string;
    expire_at: string; // ISO 8601 格式
    requires_two_factor?: boolean; // true = 该用户启用了 2FA，需带 totp_code 重新提交
}

/**
 * 修改密码请求
 */
export interface ChangePasswordRequest {
    old_password: string;
    new_password: string;
}

/**
 * 修改用户名请求
 */
export interface ChangeUsernameRequest {
    new_username: string;
}

/**
 * 认证状态 Store
 */
interface AuthState {
    isAuthenticated: boolean;
    isLoading: boolean;
    isAPIKeyAuth: boolean;
    token: string | null;
    expireAt: string | null;

    // Actions
    setAuth: (token: string, expireAt: string) => void;
    setAPIKeyAuth: (apiKey: string) => void;
    checkAuth: () => Promise<void>;
    logout: () => Promise<void>;
}

/**
 * 认证状态管理 Store（使用 zustand + persist）
 */
export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            isAuthenticated: false,
            isLoading: true,
            isAPIKeyAuth: false,
            token: null,
            expireAt: null,

            setAuth: (token: string, expireAt: string) => {
                set({
                    isAuthenticated: true,
                    isAPIKeyAuth: false,
                    token,
                    expireAt,
                    isLoading: false
                });
            },

            setAPIKeyAuth: (apiKey: string) => {
                set({
                    isAuthenticated: true,
                    isAPIKeyAuth: true,
                    token: apiKey,
                    expireAt: null,
                    isLoading: false
                });
            },

            checkAuth: async () => {
                const { token, expireAt, isAPIKeyAuth } = get();

                if (!token) {
                    set({ isAuthenticated: false, isLoading: false });
                    return;
                }

                // API Key 不检查本地过期时间
                if (!isAPIKeyAuth) {
                    if (!expireAt || Date.now() >= new Date(expireAt).getTime()) {
                        get().logout();
                        return;
                    }
                }

                try {
                    // API Key 模式只需校验 key 是否有效即可
                    const endpoint = isAPIKeyAuth ? '/api/v1/apikey/login' : '/api/v1/user/status';
                    await apiClient.get<unknown>(endpoint);
                    set({ isAuthenticated: true, isLoading: false });
                } catch (error) {
                    logger.error('认证验证失败:', error);
                    get().logout();
                }
            },

            logout: async () => {
                // Server-side cookie clear (WO-023 缺陷 B). Must hit the server:
                // the JWT lives in an HttpOnly cookie, and extractToken reads it
                // before the Authorization header, so clearing only the local
                // zustand state left the cookie authorizing every subsequent
                // request as this user until the JWT TTL elapsed (up to 90d).
                //
                // API-key mode has no server cookie to clear, but we still call
                // — the endpoint is a no-op there and local-state clear is the
                // real work. Failures (network, 5xx) must NOT block the local
                // clear, otherwise a user is stuck "logged in" with no way out;
                // the cookie will simply expire on its own if the clear call
                // failed to land.
                if (!get().isAPIKeyAuth) {
                    try {
                        await apiClient.post('/api/v1/user/logout');
                    } catch {
                        // ignore — clear local state regardless below
                    }
                }
                set({
                    isAuthenticated: false,
                    isAPIKeyAuth: false,
                    token: null,
                    expireAt: null,
                    isLoading: false
                });
                // WO-029 defect 1: drop every react-query cache entry on logout.
                // Query keys carry no identity, so on a shared device the next
                // visitor would see the previous user's balance, API keys,
                // usage and logs within staleTime. Both logout paths (the
                // explicit button and the 401 auto-logout in client.ts) funnel
                // through this action, so clearing here covers both.
                //
                // clear() over removeQueries(): it also wipes mutation state
                // (pending isPending flags, cached mutation results), which
                // removeQueries leaves behind and the next identity has no
                // business inheriting.
                //
                // After set(), not before: set() flips isAuthenticated, which
                // triggers re-renders; anything that fires a fresh fetch in
                // response must not have its result evicted by a cache clear
                // racing behind it. Clearing after the state flip also means a
                // re-render that happens mid-clear observes an empty cache
                // rather than the previous identity's data.
                //
                // cancelQueries first: fetches already in flight for the old
                // session would otherwise resolve after clear() and write the
                // previous identity's data straight back into the fresh cache.
                // Cancelled queries revert silently (no error state, no toast).
                void queryClientInstance.cancelQueries();
                queryClientInstance.clear();
            }
        }),
        {
            name: 'auth-storage',
            partialize: (state) => ({
                token: state.token,
                expireAt: state.expireAt,
                isAPIKeyAuth: state.isAPIKeyAuth,
            })
        }
    )
);

// 注册 auth store getter 到 apiClient
if (typeof window !== 'undefined') {
    setAuthStoreGetter(() => {
        const state = useAuthStore.getState();
        return {
            token: state.token,
            logout: state.logout
        };
    });
}

/**
 * 用户登录 Hook
 * 
 * @example
 * const login = useLogin();
 * login.mutate({ username: 'admin', password: '123456', expire: 86400 });
 * 
 * if (login.isPending) return <Loading />;
 * if (login.isError) return <Error message={login.error.message} />;
 */
export function useLogin() {
    const { setAuth } = useAuthStore();

    return useMutation({
        mutationFn: async (data: UserLoginRequest) => {
            return apiClient.post<UserLoginResponse>('/api/v1/user/login', data, undefined, false);
        },
        onSuccess: (data) => {
            // requires_two_factor 表示需要 TOTP，此时后端不发 token，不要写入 auth
            if (data.requires_two_factor) return;
            // 保存到 zustand store
            setAuth(data.token, data.expire_at);
        },
        onError: (error) => {
            logger.error('登录失败:', error);
        },
    });
}

/**
 * 公开注册 Hook（仅商业模式开放）。成功即自动登录（返回 token）。
 */
export function useRegister() {
    const { setAuth } = useAuthStore();

    return useMutation({
        mutationFn: async (data: UserRegisterRequest) => {
            return apiClient.post<UserLoginResponse>('/api/v1/user/register', data, undefined, false);
        },
        onSuccess: (data) => {
            setAuth(data.token, data.expire_at);
        },
        onError: (error) => {
            logger.error('注册失败:', error);
        },
    });
}

/**
 * WO-026 阶段 B：忘记密码 —— 请求重置码。
 * 后端枚举防护：无论邮箱是否存在都返回 200 + 相同响应体，前端**不得**根据
 * 响应推断邮箱是否存在（也不应该向用户展示"该邮箱不存在"之类的提示）。
 */
export function useForgotPassword() {
    return useMutation({
        mutationFn: async (email: string) =>
            apiClient.post<{ message?: string }>('/api/v1/user/forgot-password', { email }, undefined, false),
    });
}

/**
 * WO-026 阶段 B：忘记密码 —— 用一次性码 + 新密码完成重置。
 * 成功后后端会清掉 JWT cookie；失败统一返回"验证码错误或已过期"。
 */
export function useResetPassword() {
    return useMutation({
        mutationFn: async (data: { email: string; code: string; new_password: string }) =>
            apiClient.post<{ message?: string }>('/api/v1/user/reset-password', data, undefined, false),
    });
}

/** Lodestar：当前登录用户（驱动按角色分流——管理控制台 vs 用户自助门户） */
export interface CurrentUser {
    id: number;
    username: string;
    role: string;
    quota: number;
    used_quota: number;
}

/**
 * /user/me 的 query options。抽成独立函数是为了可测：本仓 node --test 无 DOM、
 * 无 renderer，测不了 hook，但可以把这份 options 交给真 QueryObserver 观测
 * "到底有没有发出请求"（见 current-user-gate.test.ts）。
 *
 * ★ API Key 会话必须排除，否则密钥登录永远失败：GET /api/v1/user/me 挂在 JWT
 * 中间件后面（internal/server/handlers/user.go:103），拿 API Key 去打必得 401，
 * 而 client.ts 的全局 401 分支直接调 logout() ——于是 /apikey/login 刚返回 200、
 * 会话在几毫秒后被自己清掉，用户看到的是"密钥登录永远登不进"。
 * 同仓另外两处早已带这个守卫（apikey.ts:72、app.tsx 的 settings 查询），唯此处漏了。
 *
 * 配套改动：app.tsx 的 bootstrap 门必须同时排除 API Key 会话——disabled query 的
 * isPending 恒为 true，只改这里会把"登录即踢出"换成"卡在全屏 loader"。
 */
export function getCurrentUserQueryOptions(session: {
    isAuthenticated: boolean;
    isAPIKeyAuth: boolean;
}) {
    return {
        queryKey: ['user', 'me'] as const,
        queryFn: async () => apiClient.get<CurrentUser>('/api/v1/user/me'),
        enabled: session.isAuthenticated && !session.isAPIKeyAuth,
        staleTime: 60_000,
        retry: false,
        refetchOnWindowFocus: false,
    };
}

export function useCurrentUser() {
    const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
    const isAPIKeyAuth = useAuthStore((s) => s.isAPIKeyAuth);
    return useQuery(getCurrentUserQueryOptions({ isAuthenticated, isAPIKeyAuth }));
}

/** staff（admin/editor）见完整控制台；其他（viewer，含商业注册用户）见受限门户 */
export function isStaffRole(role?: string): boolean {
    return role === 'admin' || role === 'editor';
}

/** 发送邮箱验证码（注册前，公开） */
export function useSendEmailCode() {
    return useMutation({
        mutationFn: async (emailAddr: string) =>
            apiClient.post('/api/v1/user/send-email-code', { email: emailAddr }, undefined, false),
    });
}

/** Lodestar：每用户 UI 偏好（绑账户，跨设备一致） */
export interface UserPreferences {
    themePreset?: string;
}

export function useUserPreferences() {
    return useQuery({
        queryKey: ['user', 'preferences'],
        queryFn: async () => {
            const res = await apiClient.get<{ preferences: string }>('/api/v1/user/preferences');
            try {
                return (JSON.parse(res.preferences || '{}') ?? {}) as UserPreferences;
            } catch (e) { console.error(e);
                return {} as UserPreferences;
            }
        },
        staleTime: Infinity,
        retry: false,
        refetchOnWindowFocus: false,
    });
}

export function useSetUserPreferences() {
    return useMutation({
        mutationFn: async (prefs: UserPreferences) => {
            return apiClient.post('/api/v1/user/preferences', { preferences: JSON.stringify(prefs) });
        },
        onError: (error) => {
            logger.error('保存偏好失败:', error);
        },
    });
}

/**
 * 修改密码 Hook
 * 
 * @example
 * const changePassword = useChangePassword();
 * changePassword.mutate({ oldPassword: '123', newPassword: '456' });
 */
export function useChangePassword() {
    return useMutation({
        mutationFn: async (data: { oldPassword: string; newPassword: string }) => {
            const payload: ChangePasswordRequest = {
                old_password: data.oldPassword,
                new_password: data.newPassword,
            };
            return apiClient.post<string>('/api/v1/user/change-password', payload);
        },
        onSuccess: (message) => {
            logger.log('密码修改成功:', message);
        },
        onError: (error) => {
            logger.error('密码修改失败:', error);
        },
    });
}

/**
 * 修改用户名 Hook
 * 
 * @example
 * const changeUsername = useChangeUsername();
 * changeUsername.mutate({ newUsername: 'newname' });
 */
export function useChangeUsername() {
    return useMutation({
        mutationFn: async (data: { newUsername: string }) => {
            const payload: ChangeUsernameRequest = {
                new_username: data.newUsername,
            };
            return apiClient.post<string>('/api/v1/user/change-username', payload);
        },
        onSuccess: (message) => {
            logger.log('用户名修改成功:', message);
        },
        onError: (error) => {
            logger.error('用户名修改失败:', error);
        },
    });
}

/**
 * 认证状态和方法 Hook
 * 
 * @example
 * const auth = useAuth();
 * 
 * if (auth.isAuthenticated) {
 *   // 已登录
 * }
 * 
 * auth.logout(); // 登出
 */
export function useAuth() {
    const store = useAuthStore();
    const { checkAuth, isLoading } = store;

    // 只在首次挂载时检查认证状态
    useEffect(() => {
        if (isLoading) {
            checkAuth();
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []); // 有意只在挂载时执行一次

    return {
        isAuthenticated: store.isAuthenticated,
        isAPIKeyAuth: store.isAPIKeyAuth,
        isLoading: store.isLoading,
        logout: store.logout,
    };
}

export interface UserInfo {
    id: number;
    username: string;
    role: string;
}

export interface UserCreateRequest {
    username: string;
    password: string;
    role: string;
}

export function useUserList() {
    return useQuery({
        queryKey: ['users', 'list'],
        queryFn: async () => apiClient.get<UserInfo[]>('/api/v1/user/list'),
        refetchInterval: REFETCH_INTERVAL_DEFAULT,
    });
}

export function useCreateUser() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: UserCreateRequest) => {
            return apiClient.post<null>('/api/v1/user/create', data);
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['users', 'list'] });
        },
        onError: (error) => {
            logger.error('User create failed:', error);
        },
    });
}

export function useUpdateUserRole() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: { id: number; role: string }) => {
            return apiClient.post<null>('/api/v1/user/update-role', data);
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['users', 'list'] });
        },
        onError: (error) => {
            logger.error('Role update failed:', error);
        },
    });
}

export function useDeleteUser() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => {
            return apiClient.delete<null>(`/api/v1/user/delete/${id}`);
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['users', 'list'] });
        },
        onError: (error) => {
            logger.error('User delete failed:', error);
        },
    });
}

