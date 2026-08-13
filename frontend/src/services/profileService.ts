const baseURL = import.meta.env.VITE_BASE_URL;

const getErrorMessage = async (response: Response, fallback: string) => {
  const body = await response.json().catch(() => null);
  return body?.error || fallback;
};

interface PresignedAvatarUpload {
  uploadUrl: string;
  objectKey: string;
  headers: Record<string, string>;
  expiresAt: string;
}

export const getProfile = async (token: string) => {
  const response = await fetch(`${baseURL}/user/fetchprofile`, {
    method: "GET",
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) throw new Error("Failed to fetch profile");
  return response.json();
};

export const updateProfile = async (
  token: string,
  displayName: string,
  bio: string,
  twitter?: string,
  instagram?: string,
  linkedin?: string
) => {
  const response = await fetch(`${baseURL}/user/updateprofile`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ displayName, bio, twitter, instagram, linkedin }),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || "Failed to update profile");
  }
  return response.json();
};

export const checkDisplayNameAvailability = async (
  token: string,
  displayName: string
): Promise<{ available: boolean }> => {
  const response = await fetch(
    `${baseURL}/user/check-displayname?displayName=${encodeURIComponent(displayName)}`,
    {
      method: "GET",
      headers: { Authorization: `Bearer ${token}` },
    }
  );
  if (!response.ok) throw new Error("Failed to check display name");
  return response.json();
};


export const uploadAvatar = async (token: string, file: File) => {
  const presignResponse = await fetch(`${baseURL}/user/avatar-upload/presign`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ contentType: file.type, fileSize: file.size }),
  });
  if (!presignResponse.ok) {
    throw new Error(
      await getErrorMessage(presignResponse, "Failed to prepare avatar upload")
    );
  }

  const presigned = (await presignResponse.json()) as PresignedAvatarUpload;
  const uploadResponse = await fetch(presigned.uploadUrl, {
    method: "PUT",
    headers: presigned.headers,
    body: file,
  });
  if (!uploadResponse.ok) {
    throw new Error("Failed to upload avatar to storage");
  }

  const confirmResponse = await fetch(`${baseURL}/user/avatar-upload/confirm`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ objectKey: presigned.objectKey }),
  });
  if (!confirmResponse.ok) {
    throw new Error(
      await getErrorMessage(confirmResponse, "Failed to confirm avatar upload")
    );
  }

  return confirmResponse.json() as Promise<{
    message: string;
    avatarUrl: string;
  }>;
};

export const setGeneratedAvatar = async (token: string, avatarUrl: string) => {
  const response = await fetch(`${baseURL}/user/avatar`, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({ avatarUrl }),
  });
  if (!response.ok) {
    throw new Error(
      await getErrorMessage(response, "Failed to update generated avatar")
    );
  }
  return response.json() as Promise<{ message: string; avatarUrl: string }>;
};
export const getLeaderboard = async () => {
  const response = await fetch(`${baseURL}/leaderboard`, {
    method: "GET",
  });
  if (!response.ok) throw new Error("Failed to fetch leaderboard");
  return response.json();
};