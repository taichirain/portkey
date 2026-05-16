import React, { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';
import { apiService } from '../services/api';
import { LoginRequest, LoginResponse, User, SystemRoles } from '../types';

interface AuthContextType {
  isAuthenticated: boolean;
  user: User | null;
  login: (request: LoginRequest) => Promise<LoginResponse>;
  logout: () => void;
  loading: boolean;
  hasRole: (role: string) => boolean;
  hasAnyRole: (roles: string[]) => boolean;
  hasAllRoles: (roles: string[]) => boolean;
  hasPermission: (permission: string) => boolean;
  hasAnyPermission: (permissions: string[]) => boolean;
  hasAllPermissions: (permissions: string[]) => boolean;
  isSuperAdmin: boolean;
  isTenantAdmin: boolean;
  tenantId: string | null;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const parseStoredArray = (value: string | null): string[] => {
  if (!value) return [];
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
};

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const token = apiService.getToken();
    if (token) {
      setIsAuthenticated(true);
      const adminId = localStorage.getItem('admin_id');
      const username = localStorage.getItem('username');
      const tenantId = localStorage.getItem('tenant_id');
      const roles = parseStoredArray(localStorage.getItem('roles'));
      const permissions = parseStoredArray(localStorage.getItem('permissions'));
      
      if (adminId && username) {
        setUser({
          id: adminId,
          username,
          tenantId: tenantId || undefined,
          roles,
          permissions,
        });
      }
    }
    setLoading(false);
  }, []);

  const login = async (request: LoginRequest): Promise<LoginResponse> => {
    const response = await apiService.login(request);
    const roles = response.roles || [];
    const permissions = response.permissions || [];
    
    setIsAuthenticated(true);
    setUser({
      id: response.admin_id,
      username: response.username,
      tenantId: response.tenant_id,
      roles,
      permissions,
    });
    
    localStorage.setItem('admin_id', response.admin_id);
    localStorage.setItem('username', response.username);
    if (response.tenant_id) {
      localStorage.setItem('tenant_id', response.tenant_id);
    }
    localStorage.setItem('roles', JSON.stringify(roles));
    localStorage.setItem('permissions', JSON.stringify(permissions));
    
    return response;
  };

  const logout = () => {
    apiService.logout();
    setIsAuthenticated(false);
    setUser(null);
    localStorage.removeItem('admin_id');
    localStorage.removeItem('username');
    localStorage.removeItem('tenant_id');
    localStorage.removeItem('roles');
    localStorage.removeItem('permissions');
  };

  const hasRole = useCallback((role: string): boolean => {
    if (!user) return false;
    return user.roles.includes(role);
  }, [user]);

  const hasAnyRole = useCallback((roles: string[]): boolean => {
    if (!user) return false;
    return roles.some(role => user.roles.includes(role));
  }, [user]);

  const hasAllRoles = useCallback((roles: string[]): boolean => {
    if (!user) return false;
    return roles.every(role => user.roles.includes(role));
  }, [user]);

  const hasPermission = useCallback((permission: string): boolean => {
    if (!user) return false;
    if (hasRole(SystemRoles.SUPER_ADMIN)) return true;
    return user.permissions.includes(permission);
  }, [user, hasRole]);

  const hasAnyPermission = useCallback((permissions: string[]): boolean => {
    if (!user) return false;
    if (hasRole(SystemRoles.SUPER_ADMIN)) return true;
    return permissions.some(perm => user.permissions.includes(perm));
  }, [user, hasRole]);

  const hasAllPermissions = useCallback((permissions: string[]): boolean => {
    if (!user) return false;
    if (hasRole(SystemRoles.SUPER_ADMIN)) return true;
    return permissions.every(perm => user.permissions.includes(perm));
  }, [user, hasRole]);

  const isSuperAdmin = hasRole(SystemRoles.SUPER_ADMIN);
  const isTenantAdmin = hasAnyRole([SystemRoles.SUPER_ADMIN, SystemRoles.TENANT_ADMIN]);
  const tenantId = user?.tenantId || null;

  return (
    <AuthContext.Provider
      value={{
        isAuthenticated,
        user,
        login,
        logout,
        loading,
        hasRole,
        hasAnyRole,
        hasAllRoles,
        hasPermission,
        hasAnyPermission,
        hasAllPermissions,
        isSuperAdmin,
        isTenantAdmin,
        tenantId,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};
